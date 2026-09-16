package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeEnumMarkerFixture writes a package whose only registration is a bare
// Declare(Owner.Schema), so every enum in it must be discovered from the
// func (T) enum() marker alone.
func writeEnumMarkerFixture(t *testing.T, types string) string {
	t.Helper()
	return newFixture(t, map[string]string{
		"types.go": "package fixture\n\n" + types,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.Declare(Owner.Schema)
`,
	})
}

// TestEnumMarkerDiagnosticsNameTheType covers every rejected marker shape
// from issue #86: a pointer receiver, a wrong signature, and a marked type
// with no typed constants. Each diagnostic names the offending type.
func TestEnumMarkerDiagnosticsNameTheType(t *testing.T) {
	for _, test := range []struct {
		name  string
		types string
		want  string
	}{
		{
			name: "pointer receiver",
			types: `type Color string
func (*Color) enum() {}
const ColorRed Color = "red"
type Owner struct { Color Color ` + "`json:\"color\"`" + ` }
`,
			want: "enum marker on Color at ",
		},
		{
			name: "wrong signature",
			types: `type Color string
func (Color) enum(int) string { return "" }
const ColorRed Color = "red"
type Owner struct { Color Color ` + "`json:\"color\"`" + ` }
`,
			want: "enum marker on Color at ",
		},
		{
			name: "zero typed constants",
			types: `type Color string
func (Color) enum() {}
const NotAColor = "red"
type Owner struct { Color Color ` + "`json:\"color\"`" + ` }
`,
			want: "enum type Color at ",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeEnumMarkerFixture(t, test.types)
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.ErrorContains(t, err, test.want)
			require.ErrorContains(t, err, "types.go")
			_, statErr := os.Stat(filepath.Join(targetDir, "jsonschema_gen.go"))
			require.True(t, os.IsNotExist(statErr), "generation must not write output on a marker diagnostic")
		})
	}
}

// TestEnumMarkerSharedAcrossStructsAndSlices proves the marker is a
// property of the type: two owners, a slice element, and an Optional
// wrapper all render the same enum from a single marker with no field-level
// declaration anywhere.
func TestEnumMarkerSharedAcrossStructsAndSlices(t *testing.T) {
	targetDir := writeEnumMarkerFixture(t, `import "github.com/tylergannon/polytype"

type Status string

func (Status) enum() {}

const (
	Ready   Status = "ready"
	Waiting Status = "waiting"
)

type Other struct {
	Status Status `+"`json:\"status\"`"+`
}

type Owner struct {
	Status  Status                    `+"`json:\"status\"`"+`
	History []Status                  `+"`json:\"history\"`"+`
	Next    polytype.Optional[Status] `+"`json:\"next,omitzero\"`"+`
	Other   Other                     `+"`json:\"other\"`"+`
}
`)
	require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))
	schema, err := os.ReadFile(filepath.Join(targetDir, "jsonschema", "Owner.json"))
	require.NoError(t, err)
	want := `"enum":["ready","waiting"]`
	require.Equal(t, 4, countOccurrences(string(schema), want), "expected every use of Status to be an enum:\n%s", schema)
}

func countOccurrences(haystack, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
		}
	}
	return count
}

// TestEnumMarkerAssertionUsesFirstConstant pins the spelling of the marker
// assertion in the generated file: each marked type is referenced through
// its first typed constant in declaration order (unexported is fine within
// the package), never through *new(T).
func TestEnumMarkerAssertionUsesFirstConstant(t *testing.T) {
	targetDir := writeEnumMarkerFixture(t, `type Status string

func (Status) enum() {}

const (
	Ready   Status = "ready"
	Waiting Status = "waiting"
)

type mode int

func (mode) enum() {}

const modeFast mode = 1
const modeSlow mode = 2

type Owner struct {
	Status Status `+"`json:\"status\"`"+`
	Mode   mode   `+"`json:\"mode\"`"+`
}
`)
	require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))
	generated, err := os.ReadFile(filepath.Join(targetDir, "jsonschema_gen.go"))
	require.NoError(t, err)
	require.Contains(t, string(generated), "_ interface{ enum() } = Ready\n")
	require.Contains(t, string(generated), "_ interface{ enum() } = modeFast\n")
	require.NotContains(t, string(generated), "*new(")
}
