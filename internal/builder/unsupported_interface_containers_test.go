package builder

import (
	"path/filepath"
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
			name: "named fixed array field",
			body: commonTypes + `
type Variants [2]Variant

type Owner struct {
	Values Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "named nested slice field",
			body: commonTypes + `
type Variants [][]Variant

type Owner struct {
	Values Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "named slice of pointer field",
			body: commonTypes + `
type Variants []*Variant

type Owner struct {
	Values Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "optional named slice field",
			body: commonTypes + `
type Variants []Variant

type Owner struct {
	Values polytype.Optional[Variants] ` + "`json:\"values,omitzero\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "nullable named slice field",
			body: commonTypes + `
type Variants []Variant

type Owner struct {
	Values polytype.Nullable[Variants] ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "slice of named slice field",
			body: commonTypes + `
type Variants []Variant

type Owner struct {
	Values []Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
		},
		{
			name: "named slice through a second named type",
			body: commonTypes + `
type Variants []Variant

type Aliased Variants

type Owner struct {
	Values Aliased ` + "`json:\"values\"`" + `
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

	fixtures := make([]fixtureCase, 0, len(tests))
	for _, test := range tests {
		fixtures = append(fixtures, fixtureCase{name: test.name, files: unsupportedInterfaceFixtureFiles(test.body)})
	}
	cases := loadFixtureCases(t, fixtures)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			loaded := cases[test.name]
			require.Empty(t, loaded.pkg.Errors)
			scan, err := syntax.LoadPackage(loaded.pkg)
			require.NoError(t, err)
			require.NotEmpty(t, scan.SchemaMethods)

			_, err = New(loaded.pkg)
			require.ErrorContains(t, err, "arrays/slices of registered interfaces are not yet supported")
			require.Contains(t, err.Error(), loaded.dir)
		})
	}
}

// A named container of a union declared in a dependency package is refused
// with the same diagnostic, naming the declaration in its own package.
func TestUnsupportedRegisteredInterfaceContainerInDependencyPackage(t *testing.T) {
	t.Parallel()

	module := newFixtureModule(t)
	writeFixturePackage(t, module, "variants", map[string]string{
		"variants.go": `package variants

type Variant interface{ variant() }

type First struct {
	Name string ` + "`json:\"name\"`" + `
}

func (First) variant() {}

type Variants []Variant
`,
	})
	targetDir := writeFixturePackage(t, module, "", map[string]string{
		"schema.go": `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"example.com/typegrammarfixture/variants"
	"github.com/tylergannon/polytype"
)

type Owner struct {
	Values variants.Variants ` + "`json:\"values\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`,
	})

	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	_, err = New(pkgs[0])
	require.ErrorContains(t, err, "arrays/slices of registered interfaces are not yet supported")
	require.Contains(t, err.Error(), filepath.Join(module, "variants", "variants.go"))
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
	require.NotContains(t, builder.ownerCodecs, "Owner")
}

func writeUnsupportedInterfaceFixture(t *testing.T, body string) string {
	t.Helper()
	return newFixture(t, unsupportedInterfaceFixtureFiles(body))
}

// unsupportedInterfaceFixtureFiles is writeUnsupportedInterfaceFixture's file
// set, split out so a table of cases can be materialized into one module and
// loaded together.
func unsupportedInterfaceFixtureFiles(body string) map[string]string {
	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)
` + body
	return map[string]string{
		"schema.go": source,
	}
}
