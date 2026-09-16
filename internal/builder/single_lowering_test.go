package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// The builder lowers every declared root once, and each output is a
// projection of that one lowering. These tests pin the behaviors that only
// exist because there is a single lowering: shapes the schema renders that
// the strict grammar backends refuse, and the codec plan read off the IR.

func TestProvidedFieldsLowerForSchemaAndRefuseStrictly(t *testing.T) {
	builder := loadTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type External struct {
	Value string `+"`json:\"value\"`"+`
}

type Root struct {
	Linked External `+"`json:\"linked\" jsonschema:\"ref=https://example.test/external.json\"`"+`
	Maybe polytype.Optional[External] `+"`json:\"maybe,omitzero\" jsonschema:\"ref=https://example.test/external.json\"`"+`
	Provided string `+"`json:\"provided\"`"+`
	Plain string `+"`json:\"plain\"`"+`
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

func provide(string) json.Marshaler { return json.RawMessage(`+"`\"provided\"`"+`) }

var _ = polytype.Declare(Root.Schema).Function(polytype.Field[Root, string]("Provided"), provide)
`)

	root := requireDefinition(t, builder.lowered.defs, "Root")
	object, ok := root.Type.(*typegrammar.Object)
	require.True(t, ok)
	require.Equal(t, []string{"linked", "maybe", "provided", "plain"}, fieldJSONNames(object.Fields))
	require.Equal(t, &typegrammar.Provided{Ref: "https://example.test/external.json"}, requireField(t, object.Fields, "linked").Value)
	require.Equal(t, &typegrammar.Provided{Ref: "https://example.test/external.json", Optional: true}, requireField(t, object.Fields, "maybe").Value)
	require.Equal(t, &typegrammar.Provided{}, requireField(t, object.Fields, "provided").Value)
	require.IsType(t, &typegrammar.Required{}, requireField(t, object.Fields, "plain").Value)

	// The strict entry point reports the first refusal, in field order.
	_, err := builder.TypeDefinitions()
	require.ErrorContains(t, err, "Root.Linked")
	require.ErrorContains(t, err, "explicit schema ref with no resolved static type target")
}

func TestInlineStructsCarryRefsButNoProviders(t *testing.T) {
	builder := loadTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Root struct {
	Inner struct {
		// Value shares the provided field's Go name but is not that field.
		Value string `+"`json:\"value\"`"+`
		Linked string `+"`json:\"linked\" jsonschema:\"ref=https://example.test/linked.json\"`"+`
	} `+"`json:\"inner\"`"+`
	Value string `+"`json:\"value\"`"+`
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

func provide(string) json.Marshaler { return json.RawMessage(`+"`\"provided\"`"+`) }

var _ = polytype.Declare(Root.Schema).Function(polytype.Field[Root, string]("Value"), provide)
`)

	root := loweredObject(t, builder, "Root")
	require.Equal(t, []string{"inner", "value"}, fieldJSONNames(root.Fields))
	require.Equal(t, &typegrammar.Provided{}, requireField(t, root.Fields, "value").Value, "the provider applies to the named owner's own field")

	inner := fieldType[*typegrammar.Object](t, requireField(t, root.Fields, "inner").Value)
	require.Equal(t, []string{"value", "linked"}, fieldJSONNames(inner.Fields))
	value := requireField(t, inner.Fields, "value")
	require.Equal(t, &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}}, value.Value, "the inline struct's same-named field is not provided")
	require.Equal(t, "Value shares the provided field's Go name but is not that field.", value.Description)
	require.Equal(t, &typegrammar.Provided{Ref: "https://example.test/linked.json"}, requireField(t, inner.Fields, "linked").Value)
}

func TestShadowedPromotedGoNameIsRefused(t *testing.T) {
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Base struct {
	ID string `+"`json:\"base_id\"`"+`
}

type Root struct {
	ID int `+"`json:\"id\"`"+`
	Base
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Root.Schema)
`)
	packages, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	_, err = New(packages[0])
	require.ErrorContains(t, err, `promoted field Root.Base.ID shares Go field name "ID" with Root.ID at `)
	require.ErrorContains(t, err, "fixture.go:12:5; a shadowed promoted field is outside the static type grammar")
}

func TestProvidedFieldsRefuseNullableWrappers(t *testing.T) {
	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type External struct {
	Value string ` + "`json:\"value\"`" + `
}

type Root struct {
	Linked polytype.Nullable[External] ` + "`json:\"linked\" jsonschema:\"ref=https://example.test/external.json\"`" + `
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Root.Schema)
`
	dir := writeTypeGrammarFixture(t, source)
	packages, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	_, err = New(packages[0])
	require.ErrorContains(t, err, "polytype.Nullable does not support explicit refs")
}

func TestSealedInterfaceRootLowersAsUnion(t *testing.T) {
	builder := loadTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Shape interface{ shape() }

type Circle struct {
	Radius float64 `+"`json:\"radius\"`"+`
}

func (Circle) shape() {}

type Square struct {
	Side float64 `+"`json:\"side\"`"+`
}

func (*Square) shape() {}

func ShapeSchema(Shape) json.RawMessage { panic("not implemented") }

var (
	_ = polytype.Declare(ShapeSchema)
	_ = polytype.SealedUnion[Shape]("kind")
)
`)

	require.Len(t, builder.lowered.roots, 1)
	root := builder.lowered.roots[0].schema
	require.NotNil(t, root.Union)
	require.Equal(t, "Shape", root.TypeName())
	require.Equal(t, "kind", root.Union.Discriminator)
	require.Len(t, root.Union.Variants, 2)
	require.Equal(t, "Circle", root.Union.Variants[0].Implementation.Name)
	require.False(t, root.Union.Variants[0].Pointer)
	require.Equal(t, "Square", root.Union.Variants[1].Implementation.Name)
	require.True(t, root.Union.Variants[1].Pointer)

	// The variants are reachable definitions, but neither has a union field,
	// so no owner codec is planned and the strict grammar refuses the root.
	require.Empty(t, builder.ownerCodecs)
	_, err := builder.TypeDefinitions()
	require.ErrorContains(t, err, "registered interface example.com/typegrammarfixture.Shape is valid only as a configured direct field")
}

func TestOwnerCodecsFollowReachabilityAndSourceOrder(t *testing.T) {
	builder := loadTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Block interface{ block() }

type Text struct {
	Text string `+"`json:\"text\"`"+`
}

func (Text) block() {}

type Level int

const (
	LevelLow Level = iota
	LevelHigh
)

func (Level) enum() {}

// Base is embedded: its union fields belong to the embedding owner's codec.
type Base struct {
	Header Block `+"`json:\"header\"`"+`
}

type Unreached struct {
	Block Block `+"`json:\"block\"`"+`
}

type Nested struct {
	Blocks []Block `+"`json:\"blocks\"`"+`
}

type Root struct {
	Body Block `+"`json:\"body\"`"+`
	Base
	Footer polytype.Optional[Block] `+"`json:\"footer,omitzero\"`"+`
	Level Level `+"`json:\"level\"`"+`
	Nested []Nested `+"`json:\"nested\"`"+`
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Root.Schema).StringerEnum(polytype.Field[Root, Level]("Level"))
`)

	require.ElementsMatch(t, []string{"Root", "Nested"}, builder.sortedOwnerCodecNames())
	require.NotContains(t, builder.ownerCodecs, "Unreached", "a definition no root reaches gets no codec")
	require.NotContains(t, builder.ownerCodecs, "Base", "an embedded struct's fields are promoted into the owner")

	root := builder.ownerCodecs["Root"]
	var (
		names     []string
		accessors []string
	)
	for _, prop := range root.UnionFields {
		names = append(names, prop.JSONName())
		accessors = append(accessors, prop.Accessor("r"))
	}
	// The owner's own fields first in declaration order, then the embedded
	// struct's, matching the order the codec template has always emitted.
	require.Equal(t, []string{"body", "footer", "header"}, names)
	require.Equal(t, []string{"r.Body", "r.Footer", "r.Base.Header"}, accessors)
	require.True(t, root.UnionFields[1].Optional)
	// The plan keeps the raw declaration (no SealedUnion override here); the
	// IR carries the effective property name.
	require.Equal(t, "", root.UnionFields[0].DiscPropName)
	require.Equal(t, "type", root.UnionFields[0].Union.Discriminator)

	require.Len(t, root.EnumFields, 1)
	require.Equal(t, "level", root.EnumFields[0].JSONName())
	require.Equal(t, "Level", root.EnumFields[0].EnumTypeName)
	require.Equal(t, []string{"LevelLow", "LevelHigh"}, func() []string {
		var out []string
		for _, entry := range root.EnumFields[0].Entries {
			out = append(out, entry.WireName)
		}
		return out
	}())

	nested := builder.ownerCodecs["Nested"]
	require.Len(t, nested.UnionFields, 1)
	require.True(t, nested.UnionFields[0].Repeated)
	require.Equal(t, nested.UnionFields[0].UnmarshalerFunc(), root.UnionFields[0].UnmarshalerFunc(), "one helper per interface identity")
}

func TestPackageErrorsAreStrictRefusalsOnly(t *testing.T) {
	// A package that fails type-checking still lowers: the schema output has
	// never depended on go/types, and the tagged fixtures rely on that. The
	// grammar backends, which do, refuse it.
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

var broken int = "not an int"

type Root struct {
	Value string `+"`json:\"value\"`"+`
}

func (Root) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Root.Schema)
`)
	packages, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	require.NotEmpty(t, packages[0].Errors)
	builder, err := New(packages[0])
	require.NoError(t, err)
	require.Equal(t, &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}}, requireField(t, loweredObject(t, builder, "Root").Fields, "value").Value)
	require.Contains(t, builder.schemas, "Root", "the schema projection still runs")
	_, err = builder.TypeDefinitions()
	require.ErrorContains(t, err, "has type-check errors")
}
