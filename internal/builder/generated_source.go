package builder

import (
	"encoding/json"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/tylergannon/polytype/internal/syntax"
)

// Ownership is a leading comment, not successful parsing of the previous
// output. Even a truncated generated file must remain disposable.
func hasGeneratedGoHeader(content []byte) bool {
	var scan scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(content))
	scan.Init(file, content, func(token.Position, string) {}, scanner.ScanComments)
	for {
		_, kind, text := scan.Scan()
		if kind != token.COMMENT {
			return false
		}
		if text == generatedGoHeader {
			return true
		}
	}
}

// Normally Polytype output already has !jsonschema and needs no overlay. A
// damaged header-owned file that lost that constraint is hidden from the Go
// loader, so it cannot contribute stale declarations or syntax errors.
func generatedSourceOverlay(dir string) (map[string][]byte, error) {
	var overlay map[string][]byte
	for _, name := range []string{syntax.GeneratedSchemaFile, syntax.GeneratedCodecFile} {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !hasGeneratedGoHeader(content) || strings.HasPrefix(string(content), "//go:build !jsonschema\n") {
			continue
		}
		if overlay == nil {
			overlay = map[string][]byte{}
		}
		overlay[path] = []byte("//go:build !jsonschema\n\npackage ignored\n")
	}
	return overlay, nil
}

// These overlays only exclude previous output. Unlike introducing a new
// compiled file, they need no packages.Config.Overlay: the default Go driver
// honors -overlay, and every file later parsed still exists on disk. This
// preserves dependency export reuse in x/tools.
func generatedOverlayFlag(overlay map[string][]byte) (string, func(), error) {
	if len(overlay) == 0 {
		return "", func() {}, nil
	}
	dir, err := os.MkdirTemp("", "polytype-source-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	replacements := map[string]string{}
	for path, content := range overlay {
		backing := filepath.Join(dir, filepath.Base(path))
		if err = os.WriteFile(backing, content, 0600); err != nil {
			cleanup()
			return "", func() {}, err
		}
		replacements[path] = backing
	}
	data, _ := json.Marshal(struct{ Replace map[string]string }{replacements})
	path := filepath.Join(dir, "overlay.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return "-overlay=" + path, cleanup, nil
}
