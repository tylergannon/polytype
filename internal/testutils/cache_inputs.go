package testutils

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TrackFixtureDependencies records, as inputs of the running test, the
// source of every package of module that the Go files under fixtureDir
// depend on, including "*.go.golden" files, which become generated source.
// Imports under an excluded path prefix, such as the fixture module's own
// packages, are skipped.
//
// A test that builds or runs fixtures in a subprocess depends on those
// packages without the go test cache knowing it: the cache keys a result on
// the linked test binary and on the files the test process itself opened. An
// edit to the fixtures' dependencies would otherwise be answered with a
// stale cached pass. Reading each dependency's directory and Go files here
// makes such an edit invalidate the result.
func TrackFixtureDependencies(t testing.TB, fixtureDir, module string, excluded ...string) {
	t.Helper()
	imports, err := fixtureImports(fixtureDir, module, excluded)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) == 0 {
		t.Fatalf("no %s imports found under %s", module, fixtureDir)
	}
	// Resolve from the test's own package directory, inside module, where
	// the imports name the module's own packages whatever the fixture's
	// go.mod says.
	args := append([]string{"list", "-deps", "-f", "{{.ImportPath}}\t{{.Dir}}"}, imports...)
	exitCode, stdout, stderr, err := RunCommand("go", ".", args...)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 0 {
		t.Fatalf("go %s: %s", strings.Join(args, " "), stderr)
	}
	for line := range strings.Lines(stdout) {
		importPath, dir, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !inModule(importPath, module) {
			continue
		}
		if err := readPackageSources(dir); err != nil {
			t.Fatal(err)
		}
	}
}

// TrackedFixtureDependencies lists the packages TrackFixtureDependencies
// resolves its dependency walk from, for tests of the tracking itself.
func TrackedFixtureDependencies(fixtureDir, module string, excluded ...string) ([]string, error) {
	return fixtureImports(fixtureDir, module, excluded)
}

func fixtureImports(fixtureDir, module string, excluded []string) ([]string, error) {
	fset := token.NewFileSet()
	wanted := make(map[string]bool)
	err := filepath.WalkDir(fixtureDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".go.golden") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !inModule(importPath, module) || slices.ContainsFunc(excluded, func(prefix string) bool { return inModule(importPath, prefix) }) {
				continue
			}
			wanted[importPath] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan fixture imports under %s: %w", fixtureDir, err)
	}
	imports := make([]string, 0, len(wanted))
	for importPath := range wanted {
		imports = append(imports, importPath)
	}
	slices.Sort(imports)
	return imports, nil
}

func inModule(importPath, module string) bool {
	return importPath == module || strings.HasPrefix(importPath, module+"/")
}

// readPackageSources opens dir and reads every Go file in it. The test
// cache hashes what a test opens, so this is what records the package.
func readPackageSources(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if _, err := os.ReadFile(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
