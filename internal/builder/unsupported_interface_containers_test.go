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

	// Each shape is a container of the union on Owner.Values. A supplied
	// schema (an explicit ref or a runtime provider) does not make the shape
	// supportable: the generated codecs adapt the field by its Go type, so
	// every decoration must be refused as well (#148).
	shapes := []struct {
		name      string
		decls     string
		fieldType string
		jsonTag   string
		// nullable shapes are also refused for any supplied schema, and may
		// report that refusal instead.
		nullable bool
	}{
		{name: "fixed array field", fieldType: "[2]Variant"},
		{name: "nested slice field", fieldType: "[][]Variant"},
		{name: "nullable slice field", fieldType: "polytype.Nullable[[]Variant]", nullable: true},
		{name: "optional slice field", fieldType: "polytype.Optional[[]Variant]", jsonTag: "values,omitzero"},
		{name: "named slice field", decls: "type Variants []Variant", fieldType: "Variants"},
		{name: "named fixed array field", decls: "type Variants [2]Variant", fieldType: "Variants"},
		{name: "named nested slice field", decls: "type Variants [][]Variant", fieldType: "Variants"},
		{name: "named slice of pointer field", decls: "type Variants []*Variant", fieldType: "Variants"},
		{name: "optional named slice field", decls: "type Variants []Variant", fieldType: "polytype.Optional[Variants]", jsonTag: "values,omitzero"},
		{name: "nullable named slice field", decls: "type Variants []Variant", fieldType: "polytype.Nullable[Variants]", nullable: true},
		{name: "slice of named slice field", decls: "type Variants []Variant", fieldType: "[]Variants"},
		{name: "named slice through a second named type", decls: "type Variants []Variant\n\ntype Aliased Variants", fieldType: "Aliased"},
	}
	decorations := []struct {
		name         string
		tag          string
		registration string
	}{
		{name: "", registration: "polytype.NewJSONSchemaMethod(Owner.Schema)"},
		{name: " with ref", tag: ` jsonschema:"ref=#/definitions/Variants"`, registration: "polytype.NewJSONSchemaMethod(Owner.Schema)"},
		{name: " with provider", registration: "polytype.NewJSONSchemaMethod(Owner.Schema, polytype.WithStructAccessorMethod(Owner{}.Values, (Owner).ValuesSchema), polytype.WithRenderProviders())"},
	}

	type test struct {
		name         string
		body         string
		otherRefusal bool
	}
	tests := []test{{
		name: "top-level named slice",
		body: commonTypes + `
type Variants []Variant

func (Variants) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Variants.Schema)
`,
	}}
	for _, shape := range shapes {
		jsonTag := shape.jsonTag
		if jsonTag == "" {
			jsonTag = "values"
		}
		for _, decoration := range decorations {
			tests = append(tests, test{
				name:         shape.name + decoration.name,
				otherRefusal: shape.nullable && decoration.name != "",
				body: commonTypes + shape.decls + `

type Owner struct {
	Values ` + shape.fieldType + " `json:\"" + jsonTag + `"` + decoration.tag + "`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

func (Owner) ValuesSchema() json.Marshaler { panic("not implemented") }

var _ = ` + decoration.registration + `
`,
			})
		}
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
			require.Error(t, err)
			if test.otherRefusal {
				require.Regexp(t, `does not support|arrays/slices of registered interfaces are not yet supported`, err.Error())
				return
			}
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
