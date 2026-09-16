package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
)

// writeEnumMarkerFixture writes a package whose only registration is a bare
// Declare(Owner.Schema), so every enum in it must be discovered from the
// func (T) enum() marker alone.
func writeEnumMarkerFixture(t *testing.T, types string) string {
	t.Helper()
	return newFixture(t, enumMarkerFixtureFiles(types))
}

// enumMarkerFixtureFiles is writeEnumMarkerFixture's file set, split out so a
// table of cases can be materialized into one module and loaded together.
func enumMarkerFixtureFiles(types string) map[string]string {
	return map[string]string{
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
	}
}

// TestEnumMarkerDiagnosticsNameTheType covers every rejected marker shape
// from issue #86: a pointer receiver, a wrong signature, and a marked type
// with no typed constants. Each diagnostic names the offending type.
func TestEnumMarkerDiagnosticsNameTheType(t *testing.T) {
	t.Parallel()
	tests := []struct {
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
	}

	fixtures := make([]fixtureCase, 0, len(tests))
	for _, test := range tests {
		fixtures = append(fixtures, fixtureCase{name: test.name, files: enumMarkerFixtureFiles(test.types)})
	}
	cases := loadFixtureCases(t, fixtures)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loaded := cases[test.name]
			err := RunLoaded(loaded.pkg, BuilderArgs{TargetDir: loaded.dir})
			require.ErrorContains(t, err, test.want)
			require.ErrorContains(t, err, "types.go")
			_, statErr := os.Stat(filepath.Join(loaded.dir, "jsonschema_gen.go"))
			require.True(t, os.IsNotExist(statErr), "generation must not write output on a marker diagnostic")
		})
	}
}

// TestEnumMarkerIsAPropertyOfTheType proves the marker alone makes a type an
// enum wherever it is used and resolves its type-level codec: two owners, a
// slice element and an Optional wrapper all lower to one value-mode enum
// with no field-level declaration anywhere, and both a string and an
// integer marked type get a marker keyed on their first typed constant in
// declaration order. The assertion and codecs rendered from these markers
// are pinned in TestRenderGoCodeEnumMarkers without a load.
func TestEnumMarkerIsAPropertyOfTheType(t *testing.T) {
	t.Parallel()
	builder := loadBuilder(t, writeEnumMarkerFixture(t, `import "github.com/tylergannon/polytype"

type Status string

func (Status) enum() {}

const (
	Ready   Status = "ready"
	Waiting Status = "waiting"
)

type mode int

func (mode) enum() {}

const modeFast mode = 1
const modeSlow mode = 2

type Other struct {
	Status Status `+"`json:\"status\"`"+`
}

type Owner struct {
	Status  Status                    `+"`json:\"status\"`"+`
	History []Status                  `+"`json:\"history\"`"+`
	Next    polytype.Optional[Status] `+"`json:\"next,omitzero\"`"+`
	Other   Other                     `+"`json:\"other\"`"+`
	Mode    mode                      `+"`json:\"mode\"`"+`
}
`))

	owner := loweredObject(t, builder, "Owner")
	other := loweredObject(t, builder, "Other")
	for _, use := range []typegrammar.Field{
		requireField(t, owner.Fields, "status"),
		requireField(t, owner.Fields, "history"),
		requireField(t, owner.Fields, "next"),
		requireField(t, other.Fields, "status"),
	} {
		enum := loweredEnum(t, builder, use.Value)
		require.Equal(t, typegrammar.EnumValues, enum.Mode, use.JSONName)
		require.Equal(t, typegrammar.String, enum.Kind, use.JSONName)
		require.Equal(t, []string{"Ready", "Waiting"}, enumMemberNames(enum.Members), use.JSONName)
	}
	require.Equal(t, typegrammar.Int, loweredEnum(t, builder, requireField(t, owner.Fields, "mode").Value).Kind)

	require.Equal(t, []EnumMarker{
		{TypeName: "Status", Constant: "Ready", Underlying: "string", IsString: true, Members: []EnumMember{
			{Constant: "Ready", Wire: `"ready"`}, {Constant: "Waiting", Wire: `"waiting"`},
		}},
		{TypeName: "mode", Constant: "modeFast", Underlying: "int", Members: []EnumMember{
			{Constant: "modeFast", Wire: "1"}, {Constant: "modeSlow", Wire: "2"},
		}},
	}, builder.enumMarkers())
}
