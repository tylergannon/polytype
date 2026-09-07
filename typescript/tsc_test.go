package typescript

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
)

// permissiveType catches an `any` or `unknown` fallback on a word boundary, so
// a legitimate identifier ending in those letters does not look like one.
var permissiveType = regexp.MustCompile(`\b(?:any|unknown)\b`)

// edgeDefinitions holds only the projection edge cases the other tests in this
// package do not already cover: a heavily escaped string serving as a union
// discriminator (and therefore appearing inside `Omit<>`), a Unicode name that
// encodes onto a literal name already in the graph, and a definition
// description carrying both a comment terminator and a line separator.
func edgeDefinitions() typegrammar.Definitions {
	discriminator := "kind\"\\\n雪"
	payload := typegrammar.Definition{
		Name:        grammarName("Payload"),
		Description: "Payload comment closes */ then continues.\u2028Next line separator.",
		Type: &typegrammar.Object{Fields: []typegrammar.Field{
			{GoName: "Kind", JSONName: discriminator, Value: &typegrammar.Optional{Type: &typegrammar.Scalar{Kind: typegrammar.String}}},
			required("Value", "value", &typegrammar.Scalar{Kind: typegrammar.String}),
		}},
	}
	owner := definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{{
		GoName:   "Event",
		JSONName: "event",
		Value: &typegrammar.Union{
			Interface:     grammarName("Event"),
			Discriminator: discriminator,
			Variants:      []typegrammar.Variant{{Implementation: grammarName("Payload")}},
		},
	}}})
	return typegrammar.Definitions{
		payload,
		owner,
		definition("雪", &typegrammar.Object{}),
		definition("_u96EA_", &typegrammar.Object{}),
	}
}

// TestGenerateEdgeCasesCompile projects the edge-case graph, asserts the
// escaping and collision properties in Go, and then hands the actual output to
// the pinned TypeScript compiler when one is available. Compilation is what
// proves the escaped discriminator is a legal property name and a legal `Omit`
// key rather than merely the byte sequence this test expects.
func TestGenerateEdgeCasesCompile(t *testing.T) {
	t.Parallel()

	result, err := Generate(edgeDefinitions(), Options{Barrel: true})
	require.NoError(t, err)
	files := result.Files
	require.Len(t, files, 2)

	types := string(files[0].Content)
	require.Contains(t, types, `Payload comment closes *\/ then continues.\u2028Next line separator.`)
	require.Contains(t, types, `"kind\"\\\n雪"?: string;`)
	require.Contains(t, types, `Omit<Payload, "kind\"\\\n雪">`)
	require.Contains(t, types, `"kind\"\\\n雪": "";`)
	require.Equal(t, 2, strings.Count(types, "export type _u96EA_$"),
		"the Unicode name and the literal name must both be suffixed:\n%s", types)
	require.NotRegexp(t, permissiveType, types)

	tsc := findTSC(t)
	if tsc == "" {
		t.Skip("no TypeScript compiler: set $POLYTYPE_TSC or run `npm ci` at the repository root")
	}

	dir := t.TempDir()
	for _, file := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file.Name), file.Content, 0o644))
	}
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"exactOptionalPropertyTypes": true,
			"module":                     "NodeNext",
			"moduleResolution":           "NodeNext",
			"noEmit":                     true,
			"noUncheckedIndexedAccess":   true,
			"strict":                     true,
			"target":                     "ES2022",
			"types":                      []string{},
		},
		"include": []string{"*.ts"},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), config, 0o644))

	output, err := exec.Command(tsc, "--project", dir, "--pretty", "false").CombinedOutput()
	require.NoError(t, err, "tsc rejected the generated declarations:\n%s", output)
}

// findTSC returns $POLYTYPE_TSC when set, otherwise node_modules/.bin/tsc under
// the repository root (the nearest ancestor holding a go.mod), otherwise "".
func findTSC(t *testing.T) string {
	t.Helper()
	if tsc := os.Getenv("POLYTYPE_TSC"); tsc != "" {
		return tsc
	}
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			tsc := filepath.Join(dir, "node_modules", ".bin", "tsc")
			if _, err := os.Stat(tsc); err == nil {
				return tsc
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
