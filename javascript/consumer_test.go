package javascript

import (
	"bytes"
	"go/constant"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
	"github.com/tylergannon/polytype/typegrammar"
)

// consumerDefinitions is the graph the checked ESM consumer exercises: a
// recursive Node, a required-field Person, a string-valued Status enum, a
// sealed Event union with a discriminator, and an Envelope that nests Person
// beside the union.
func consumerDefinitions() typegrammar.Definitions {
	node := grammarName("Node")
	person := grammarName("Person")
	created := grammarName("Created")
	deleted := grammarName("Deleted")
	return typegrammar.Definitions{
		{Name: node, Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("Value", "value", &typegrammar.Scalar{Kind: typegrammar.String}),
			{GoName: "Next", JSONName: "next", Value: &typegrammar.Optional{Type: &typegrammar.Ref{Target: node}}},
		}}},
		{Name: person, Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("Name", "name", &typegrammar.Scalar{Kind: typegrammar.String}),
			required("Age", "age", &typegrammar.Scalar{Kind: typegrammar.Int}),
		}}},
		{Name: grammarName("Status"), Type: &typegrammar.Enum{
			GoType: grammarName("Status"),
			Kind:   typegrammar.String,
			Mode:   typegrammar.EnumValues,
			Members: []typegrammar.EnumMember{
				{Name: "Ready", Value: constant.MakeString("ready")},
				{Name: "Done", Value: constant.MakeString("done")},
			},
		}},
		{Name: created, Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("ID", "id", &typegrammar.Scalar{Kind: typegrammar.String}),
		}}},
		{Name: deleted, Type: &typegrammar.Object{}},
		{Name: grammarName("Envelope"), Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("Person", "person", &typegrammar.Ref{Target: person}),
			{GoName: "Event", JSONName: "event", Value: &typegrammar.Union{
				Interface:     grammarName("Event"),
				Discriminator: "kind",
				Variants: []typegrammar.Variant{
					{Implementation: created, Tag: "created"},
					{Implementation: deleted, Tag: ""},
				},
			}},
		}}},
	}
}

// jsDiagnostic is one line of tsgo's `file(line,col): error TSxxxx: message`
// output.
type jsDiagnostic struct {
	File    string
	Line    int
	Column  int
	Code    string
	Message string
}

var jsDiagnosticPattern = regexp.MustCompile(`^(.+)\((\d+),(\d+)\): error (TS\d+): (.*)$`)

func parseJSDiagnostics(output string) []jsDiagnostic {
	var diagnostics []jsDiagnostic
	for line := range strings.SplitSeq(output, "\n") {
		match := jsDiagnosticPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		lineNumber, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		diagnostics = append(diagnostics, jsDiagnostic{
			File:    filepath.Base(match[1]),
			Line:    lineNumber,
			Column:  column,
			Code:    match[4],
			Message: match[5],
		})
	}
	return diagnostics
}

// TestGenerateCheckedESMConsumer is the acceptance test for the JavaScript
// backend's consumer story: a real ESM file imports the generated JSDoc types
// with `import('./types.js').Name` and type-checks under tsgo's strict,
// allowJs, checkJs mode. The valid consumer, including a recursive value,
// reports nothing; each wrong-value consumer reports the specific diagnostic
// the projection is supposed to catch.
func TestGenerateCheckedESMConsumer(t *testing.T) {
	t.Parallel()

	result, err := Generate(consumerDefinitions(), Options{})
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	types := result.Files[0]

	tsgo := findNodeTool(t, "tsgo")
	oxfmt := findNodeTool(t, "oxfmt")
	oxlint := findNodeTool(t, "oxlint")
	if tsgo == "" || oxfmt == "" || oxlint == "" {
		t.Skip("tsgo, oxfmt, and oxlint require `npm ci` at the repository root")
	}

	dir := t.TempDir()
	typesPath := filepath.Join(dir, types.Name)
	require.NoError(t, os.WriteFile(typesPath, types.Content, 0o644))
	require.NoError(t, testutils.CopyDir("testdata/consumers", dir))
	validPath := filepath.Join(dir, "valid.js")

	// A valid value for every shape, including a recursive Node chain, must
	// produce zero diagnostics.
	validOutput, validErr := runTSGo(t, tsgo, dir, "valid.js")
	require.NoError(t, validErr, "valid ESM consumer failed to type-check:\n%s", validOutput)
	require.Empty(t, parseJSDiagnostics(validOutput),
		"valid ESM consumer reported diagnostics:\n%s", validOutput)

	negatives := []struct {
		name string
		want []jsDiagnostic
	}{
		{
			name: "missing_required.js",
			want: []jsDiagnostic{{Line: 5, Code: "TS2741", Message: "Property 'age' is missing"}},
		},
		{
			name: "wrong_enum.js",
			want: []jsDiagnostic{{Line: 4, Code: "TS2322", Message: `Type '"bogus"' is not assignable to type 'Status'`}},
		},
		{
			name: "wrong_discriminator.js",
			want: []jsDiagnostic{{Line: 6, Code: "TS2322", Message: `Type '"wrong"' is not assignable to type '"" | "created"'`}},
		},
		{
			name: "wrong_nested.js",
			want: []jsDiagnostic{
				{Line: 5, Code: "TS2322", Message: "Type 'string' is not assignable to type 'number'"},
				{Line: 12, Code: "TS2322", Message: "Type 'number' is not assignable to type 'string'"},
			},
		},
	}
	for _, tc := range negatives {
		t.Run(tc.name, func(t *testing.T) {
			output, runErr := runTSGo(t, tsgo, dir, tc.name)
			require.Error(t, runErr, "invalid ESM consumer unexpectedly type-checked:\n%s", output)
			diagnostics := parseJSDiagnostics(output)
			require.Len(t, diagnostics, len(tc.want),
				"diagnostics for %s:\n%s", tc.name, output)
			for i, want := range tc.want {
				require.Equal(t, tc.name, diagnostics[i].File)
				require.Equal(t, want.Line, diagnostics[i].Line,
					"line for %s diagnostic %d:\n%s", tc.name, i, output)
				require.Equal(t, want.Code, diagnostics[i].Code,
					"code for %s diagnostic %d:\n%s", tc.name, i, output)
				require.Contains(t, diagnostics[i].Message, want.Message,
					"message for %s diagnostic %d:\n%s", tc.name, i, output)
			}
		})
	}

	// oxfmt parses each generated file on stdin, exactly as the TypeScript
	// package's check does; it must not reformat the generator's own style.
	for _, path := range []string{typesPath, validPath} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		command := exec.Command(oxfmt, "--stdin-filepath", path)
		command.Stdin = bytes.NewReader(content)
		formatted, err := command.CombinedOutput()
		require.NoError(t, err, "oxfmt rejected %s:\n%s", filepath.Base(path), formatted)
		require.NotEmpty(t, formatted)
	}
	lintOutput, err := exec.Command(oxlint, "--deny-warnings", typesPath, validPath).CombinedOutput()
	require.NoError(t, err, "oxlint rejected generated JavaScript:\n%s", lintOutput)
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

// runTSGo runs the pinned native TypeScript compiler over one consumer file
// and returns its combined output. A non-zero exit is expected whenever the
// compiler reports diagnostics, so only a failure to start the process is
// treated as a test failure here; the caller asserts the diagnostics.
func runTSGo(t *testing.T, tsgo, dir string, files ...string) (string, error) {
	t.Helper()
	args := append([]string{"--noEmit", "--strict", "--allowJs", "--checkJs"}, files...)
	command := exec.Command(tsgo, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		require.ErrorAs(t, err, &exitError, "running tsgo:\n%s", output)
	}
	return string(output), err
}
