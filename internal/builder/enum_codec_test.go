package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringModeEnumRejectsDuplicateUnderlyingValuesBeforeWriting(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
const (
	ColorRed Color = 1
	ColorCrimson Color = 1
)
type Owner struct { Color Color `+"`json:\"color\"`"+` }
`, "")
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "ColorRed and ColorCrimson have duplicate underlying value 1")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestStringModeEnumRejectsDuplicateUnderlyingAliasAcrossDeclarationsBeforeWriting(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
const ColorRed Color = 1
const ColorCrimson = ColorRed
type Owner struct { Color Color `+"`json:\"color\"`"+` }
`, "")
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "ColorRed and ColorCrimson have duplicate underlying value 1")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestNonAdaptedEnumsAllowAliasValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		types  string
		option string
	}{
		{
			name: "numeric mode",
			types: `type Color int
func (Color) enum() {}
const (
	ColorRed Color = 1
	ColorCrimson Color = 1
)
type Owner struct { Color Color ` + "`json:\"color\"`" + ` }
`,
			option: "none",
		},
		{
			name: "string backed",
			types: `type Color string
const (
	ColorRed Color = "red"
	ColorCrimson Color = "red"
)
type Owner struct { Color Color ` + "`json:\"color\"`" + ` }
`,
			option: `polytype.WithStringerEnum(Owner{}.Color),`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeEnumCodecFixture(t, test.types, test.option)
			require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))
		})
	}
}

func TestStringModeEnumRejectsUnsupportedContainerBeforeWriting(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
const ColorRed Color = 1
type Owner struct { Colors []Color `+"`json:\"colors\"`"+` }
`, `polytype.WithStringerEnum(Owner{}.Colors),`)
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "supports only a direct named enum, Optional[E], or Nullable[E]")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestEnumRejectsPointerFieldBeforeWriting(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
const ColorRed Color = 1
type Owner struct { Color *Color `+"`json:\"color,omitempty\"`"+` }
`, `polytype.WithStringerEnum(Owner{}.Color),`)
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "supports only a direct named enum, Optional[E], or Nullable[E]")
	assertOwnerCollisionSentinels(t, targetDir)
}

func TestRegisteredEnumRejectsJSONStringOptionBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name         string
		option       string
		registration string
	}{
		{name: "method string mode", option: `polytype.WithStringerEnum(Owner{}.Color),`, registration: "method"},
		{name: "function string mode", option: `polytype.WithStringerEnum(Owner{}.Color),`, registration: "function"},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeJSONStringEnumFixture(t, test.option, test.registration)
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.ErrorContains(t, err, `registered enum fields do not support json:",string"`)
			assertOwnerCollisionSentinels(t, targetDir)
		})
	}
}

func TestStringModeEnumRejectsProductionJSONHooksBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name string
		hook string
	}{
		{name: "marshal", hook: `func (Color) MarshalJSON() ([]byte, error) { return []byte("1"), nil }`},
		{name: "unmarshal", hook: `func (*Color) UnmarshalJSON([]byte) error { return nil }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeEnumCodecFixture(t, `type Color int
const ColorRed Color = 1
type Owner struct { Color Color `+"`json:\"color\"`"+` }
`+test.hook+"\n", "")
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.ErrorContains(t, err, "cannot adapt string-mode enum Color because production")
			assertOwnerCollisionSentinels(t, targetDir)
		})
	}
}

func TestEnumOnlyOwnerReusesOwnerCodecCollisionAudit(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
const ColorRed Color = 1
type Owner struct { Color Color `+"`json:\"color\"`"+` }
func (Owner) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
`, "")
	err := Run(BuilderArgs{TargetDir: targetDir})
	require.ErrorContains(t, err, "handwritten production MarshalJSON")
	assertOwnerCollisionSentinels(t, targetDir)
}

func writeEnumCodecFixture(t *testing.T, types, option string) string {
	t.Helper()
	switch option {
	case "":
		option = `polytype.WithStringerEnum(Owner{}.Color),`
	case "none":
		option = ""
	}
	targetDir := newFixture(t, map[string]string{
		"types.go": "package fixture\n\n" + types,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Owner.Schema, ` + option + `)
`,
	})
	writeOwnerCollisionSentinels(t, targetDir)
	return targetDir
}

func writeJSONStringEnumFixture(t *testing.T, option, registration string) string {
	t.Helper()
	var stub, marker string
	switch registration {
	case "method":
		stub = `func (Owner) Schema() json.RawMessage { panic("not implemented") }`
		marker = `polytype.NewJSONSchemaMethod(Owner.Schema, ` + option + `)`
	case "function":
		stub = `func OwnerSchema(Owner) json.RawMessage { panic("not implemented") }`
		marker = `polytype.NewJSONSchemaFunc[Owner](OwnerSchema, ` + option + `)`
	default:
		t.Fatalf("unknown registration form %q", registration)
	}
	targetDir := newFixture(t, map[string]string{
		"types.go": `package fixture

type Color int
const ColorRed Color = 1
type Owner struct { Color Color ` + "`json:\"color,string\"`" + ` }
`,
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

` + stub + `
var _ = ` + marker + `
`,
	})
	writeOwnerCollisionSentinels(t, targetDir)
	return targetDir
}
