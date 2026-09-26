package typescript

import (
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

// TestGenerateEdgeCasesLint projects the edge-case graph, asserts its escaping
// and collision properties, and checks the actual output with the pinned
// formatter and linter when installed. CI installs both tools before go test.
func TestGenerateEdgeCasesLint(t *testing.T) {
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

	oxfmt := findNodeTool(t, "oxfmt")
	oxlint := findNodeTool(t, "oxlint")
	if oxfmt == "" || oxlint == "" {
		t.Skip("oxfmt and oxlint require `npm ci` at the repository root")
	}

	dir := t.TempDir()
	paths := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		require.NoError(t, os.WriteFile(path, file.Content, 0o644))
		paths = append(paths, path)

		// Formatting on stdin checks that oxfmt can parse the generated syntax.
		// The generator keeps its own deterministic style and bytes.
		command := exec.Command(oxfmt, "--stdin-filepath", path)
		command.Stdin = strings.NewReader(string(file.Content))
		output, err := command.CombinedOutput()
		require.NoError(t, err, "oxfmt rejected %s:\n%s", file.Name, output)
		require.NotEmpty(t, output)
	}
	output, err := exec.Command(oxlint, append([]string{"--deny-warnings"}, paths...)...).CombinedOutput()
	require.NoError(t, err, "oxlint rejected generated declarations:\n%s", output)
}

// findNodeTool locates a pinned npm development tool at the repository root.
func findNodeTool(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			tool := filepath.Join(dir, "node_modules", ".bin", name)
			if _, err := os.Stat(tool); err == nil {
				return tool
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
