package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnumMarkerEmitsTypeLevelCodecs proves that every enum()-marked type in
// the generated package receives a value-receiver MarshalJSON and a
// pointer-receiver UnmarshalJSON, in value mode: the wire form is the
// constant's own value, and a non-member is rejected by an error naming the
// type. Both admitted underlying kinds are covered by one generation.
func TestEnumMarkerEmitsTypeLevelCodecs(t *testing.T) {
	targetDir := writeEnumCodecFixture(t, `type Color int
func (Color) enum() {}
const (
	ColorRed Color = 1
	ColorBlue Color = 4
)

type Size string
func (Size) enum() {}
const (
	SizeSmall Size = "small"
	SizeLarge Size = "large"
)

type Owner struct {
	Color Color `+"`json:\"color\"`"+`
	Size Size `+"`json:\"size\"`"+`
}
`, "none")
	require.NoError(t, Run(BuilderArgs{TargetDir: targetDir}))

	generated, err := os.ReadFile(filepath.Join(targetDir, "jsonschema_gen.go"))
	require.NoError(t, err)
	code := string(generated)

	for _, expected := range []string{
		"func (__enumValue Color) MarshalJSON() ([]byte, error) {",
		"func (__enumValue *Color) UnmarshalJSON(data []byte) error {",
		"func (__enumValue Size) MarshalJSON() ([]byte, error) {",
		"func (__enumValue *Size) UnmarshalJSON(data []byte) error {",
		// Value mode: the constant's own value, not its Go identifier.
		`case ColorBlue:
		return []byte("4"), nil`,
		`case SizeLarge:
		return []byte("\"large\""), nil`,
		`is not a declared member of enum Color", int(__enumValue))`,
		`is not a declared member of enum Size", string(__enumValue))`,
	} {
		require.Contains(t, code, expected)
	}
}

// TestEnumMarkerRejectsProductionJSONMethodsBeforeWriting proves that a
// package whose production code already declares a JSON method on an
// enum-marked type is refused before anything is written, rather than
// generating a second declaration of the same method and leaving the package
// unable to compile.
func TestEnumMarkerRejectsProductionJSONMethodsBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name string
		hook string
	}{
		{name: "marshal", hook: `func (Color) MarshalJSON() ([]byte, error) { return []byte("1"), nil }`},
		{name: "unmarshal", hook: `func (*Color) UnmarshalJSON([]byte) error { return nil }`},
	} {
		t.Run(test.name, func(t *testing.T) {
			targetDir := writeEnumCodecFixture(t, `type Color int
func (Color) enum() {}
const ColorRed Color = 1
type Owner struct { Color Color `+"`json:\"color\"`"+` }
`+test.hook+"\n", "none")
			err := Run(BuilderArgs{TargetDir: targetDir})
			require.ErrorContains(t, err, "cannot generate enum codec for Color: handwritten production ")
			assertOwnerCollisionSentinels(t, targetDir)
		})
	}
}
