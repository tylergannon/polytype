package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
)

func TestUnsupportedRegisteredInterfaceContainersFailDuringGeneration(t *testing.T) {
	t.Parallel()

	const commonTypes = `
type Variant interface{ variant() }

type First struct {
	Name string ` + "`json:\"name\"`" + `
}

func (First) variant() {}
`

	tests := []struct {
		name string
		body string
	}{
		{
			name: "fixed array field",
			body: commonTypes + `
type Owner struct {
	Values [2]Variant ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "v1 nested slice field",
			body: commonTypes + `
type Owner struct {
	Values [][]Variant ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "v1 nullable slice field",
			body: commonTypes + `
type Owner struct {
	Values polytype.Nullable[[]Variant] ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "v1 optional slice field",
			body: commonTypes + `
type Owner struct {
	Values polytype.Optional[[]Variant] ` + "`json:\"values,omitzero\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "v1 named slice field",
			body: commonTypes + `
type Variants []Variant

type Owner struct {
	Values Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "fixed array field",
			body: commonTypes + `
type Owner struct {
	Values [2]Variant ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "nested slice field",
			body: commonTypes + `
type Owner struct {
	Values [][]Variant ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "nullable slice field",
			body: commonTypes + `
type Owner struct {
	Values polytype.Nullable[[]Variant] ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "optional slice field",
			body: commonTypes + `
type Owner struct {
	Values polytype.Optional[[]Variant] ` + "`json:\"values,omitzero\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "named slice field",
			body: commonTypes + `
type Variants []Variant

type Owner struct {
	Values Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "top-level named slice",
			body: commonTypes + `
type Variants []Variant

func (Variants) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Variants.Schema)
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			targetDir := writeUnsupportedInterfaceFixture(t, test.body)
			pkgs, err := syntax.Load(targetDir)
			require.NoError(t, err)
			require.Len(t, pkgs, 1)
			require.Empty(t, pkgs[0].Errors)
			scan, err := syntax.LoadPackage(pkgs[0])
			require.NoError(t, err)
			require.NotEmpty(t, scan.SchemaMethods)

			_, err = New(pkgs[0])
			require.ErrorContains(t, err, "arrays/slices of registered interfaces are not yet supported")
			require.Contains(t, err.Error(), targetDir)
		})
	}
}

func TestShadowedEmbeddedInterfaceIsNotCustomDecoded(t *testing.T) {
	t.Parallel()

	targetDir := writeUnsupportedInterfaceFixture(t, `
type Variant interface{ variant() }

type First struct{}
func (First) variant() {}

type Embedded struct {
	Value Variant `+"`json:\"value\"`"+`
}

type Owner struct {
	Embedded
	Value string `+"`json:\"value\"`"+`
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`)
	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	builder, err := New(pkgs[0])
	require.NoError(t, err)
	require.Empty(t, builder.customTypes["Owner"])
}

func writeUnsupportedInterfaceFixture(t *testing.T, body string) string {
	t.Helper()

	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)
` + body
	return newFixture(t, map[string]string{
		"schema.go": source,
	})
}
