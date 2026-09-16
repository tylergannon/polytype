package schema

import (
	"errors"
	"go/constant"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
)

const pkg = "example.com/fixture"

func name(n string) typegrammar.Name { return typegrammar.Name{PackagePath: pkg, Name: n} }

func pos(line int) token.Position { return token.Position{Filename: "fixture.go", Line: line} }

func str(v string) constant.Value { return constant.MakeString(v) }

func integer(v int64) constant.Value { return constant.MakeInt64(v) }

func object(fields ...typegrammar.Field) *typegrammar.Object {
	return &typegrammar.Object{Fields: fields}
}

func field(goName, jsonName string, value typegrammar.FieldValue) typegrammar.Field {
	return typegrammar.Field{GoName: goName, JSONName: jsonName, Value: value, Source: pos(1)}
}

func required(t typegrammar.Type) *typegrammar.Required { return &typegrammar.Required{Type: t} }

func scalarOf(kind typegrammar.ScalarKind) *typegrammar.Scalar {
	return &typegrammar.Scalar{Kind: kind}
}

func ref(n string) *typegrammar.Ref { return &typegrammar.Ref{Target: name(n)} }

func def(n string, t typegrammar.Type) typegrammar.Definition {
	return typegrammar.Definition{Name: name(n), Type: t, Source: pos(1)}
}

func described(d typegrammar.Definition, description string) typegrammar.Definition {
	d.Description = description
	return d
}

func generateOne(t *testing.T, defs typegrammar.Definitions, root Root, opts Options) string {
	t.Helper()
	schemas, err := Generate(defs, []Root{root}, opts)
	require.NoError(t, err)
	require.Len(t, schemas, 1)
	data, err := schemas[0].MarshalJSON()
	require.NoError(t, err)
	return string(data)
}

func TestGenerateScalarsTimeAndCollections(t *testing.T) {
	root := object(
		field("Count", "count", required(scalarOf(typegrammar.Int8))),
		field("Ratio", "ratio", required(scalarOf(typegrammar.Float32))),
		field("Label", "label", required(scalarOf(typegrammar.String))),
		field("Flag", "flag", required(scalarOf(typegrammar.Bool))),
		field("Ptr", "ptr", required(&typegrammar.Pointer{Element: scalarOf(typegrammar.Uint64)})),
		field("Tags", "tags", &typegrammar.Optional{Type: &typegrammar.Slice{Element: scalarOf(typegrammar.String)}}),
		field("Pair", "pair", required(&typegrammar.Array{Length: 2, Element: scalarOf(typegrammar.Int)})),
	)
	root.Fields[0].Description = "How many."
	root.Fields[5].Description = "Free-form tags."
	defs := typegrammar.Definitions{described(def("Root", root), "The root.")}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	require.Equal(t, `{"type":"object","description":"The root.","properties":{`+
		`"count":{"type":"integer","description":"How many."},`+
		`"ratio":{"type":"number"},`+
		`"label":{"type":"string"},`+
		`"flag":{"type":"boolean"},`+
		`"ptr":{"type":"integer"},`+
		`"tags":{"type":"array","description":"Free-form tags.","items":{"type":"string"}},`+
		`"pair":{"type":"array","items":{"type":"integer"}}},`+
		`"required":["count","ratio","label","flag","ptr","pair"],"additionalProperties":false}`, got)
}

func TestGenerateTimeDescription(t *testing.T) {
	root := object(
		field("At", "at", required(&typegrammar.Time{})),
		field("Until", "until", required(&typegrammar.Time{})),
	)
	root.Fields[1].Description = "Expiry"
	defs := typegrammar.Definitions{def("Root", root)}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	require.Contains(t, got, `"at":{"type":"string","description":"RFC3339 formatted date-time string (e.g., \"2006-01-02T15:04:05Z07:00\")"}`)
	require.Contains(t, got, `"until":{"type":"string","description":"Expiry. Must be an RFC3339 formatted date-time string (e.g., \"2006-01-02T15:04:05Z07:00\")"}`)
}

func TestGenerateEnumsInlineVersusReferenced(t *testing.T) {
	stringEnum := &typegrammar.Enum{GoType: name("Label"), Kind: typegrammar.String, Members: []typegrammar.EnumMember{
		{Name: "LabelFirst", Value: str("first"), Description: "The first label."},
		{Name: "LabelLast", Value: str("last")},
	}}
	intEnum := &typegrammar.Enum{GoType: name("State"), Kind: typegrammar.Int, Members: []typegrammar.EnumMember{
		{Name: "StateLow", Value: integer(0)},
		{Name: "StateHigh", Value: integer(1), Description: "Urgent."},
	}}
	namesEnum := &typegrammar.Enum{GoType: name("State"), Kind: typegrammar.Int, Mode: typegrammar.EnumNames, Members: intEnum.Members}
	root := object(
		field("Inline", "inline", required(stringEnum)),
		field("Ref", "ref", required(ref("Label"))),
		field("Ints", "ints", required(ref("State"))),
		field("Names", "names", required(namesEnum)),
		field("Many", "many", required(&typegrammar.Slice{Element: ref("Label")})),
	)
	root.Fields[1].Description = "Field comment wins."
	defs := typegrammar.Definitions{
		def("Root", root),
		described(def("Label", stringEnum), "A label."),
		def("State", intEnum),
	}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	// An inline enum is a field-local registration: wire values, no description.
	require.Contains(t, got, `"inline":{"type":"string","enum":["first","last"]}`)
	// A referencing field's comment replaces the whole referenced description,
	// member comments included.
	require.Contains(t, got, `"ref":{"type":"string","description":"Field comment wins.","enum":["first","last"]}`)
	require.Contains(t, got, `"ints":{"type":"integer","description":"1: \nUrgent.","enum":[0,1]}`)
	require.Contains(t, got, `"names":{"type":"string","enum":["StateLow","StateHigh"]}`)
	// A collection element renders the definition's own description.
	require.Contains(t, got, `"many":{"type":"array","items":{"type":"string","description":"A label.\n\nfirst: \nThe first label.","enum":["first","last"]}}`)
}

func TestGenerateNullableForms(t *testing.T) {
	enum := &typegrammar.Enum{GoType: name("Label"), Kind: typegrammar.String, Members: []typegrammar.EnumMember{{Name: "A", Value: str("a")}}}
	inner := object(field("Value", "value", required(scalarOf(typegrammar.String))))
	root := object(
		field("Count", "count", &typegrammar.Nullable{Type: scalarOf(typegrammar.Int)}),
		field("Label", "label", &typegrammar.Nullable{Type: enum}),
		field("Inner", "inner", &typegrammar.Nullable{Type: ref("Inner")}),
		field("Shared", "shared", &typegrammar.Nullable{Type: &typegrammar.Pointer{Element: ref("Shared")}}),
		field("At", "at", &typegrammar.Nullable{Type: &typegrammar.Time{}}),
	)
	defs := typegrammar.Definitions{def("Root", root), def("Inner", inner), def("Shared", inner)}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{Refs: map[typegrammar.Name]bool{name("Shared"): true}})
	require.Contains(t, got, `"count":{"type":["integer","null"]}`)
	require.Contains(t, got, `"label":{"anyOf":[{"type":"string","enum":["a"]},{"type":"null"}]}`)
	require.Contains(t, got, `"inner":{"anyOf":[{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},{"type":"null"}]}`)
	require.Contains(t, got, `"shared":{"anyOf":[{"$ref":"#/$defs/Shared"},{"type":"null"}]}`)
	require.Contains(t, got, `"at":{"type":["string","null"]`)
	require.Contains(t, got, `"required":["count","label","inner","shared","at"]`)
}

func TestGenerateRefsRenderDefs(t *testing.T) {
	leaf := object(field("Value", "value", required(scalarOf(typegrammar.String))))
	mid := object(field("Leaf", "leaf", required(ref("Leaf"))))
	root := object(
		field("First", "first", required(ref("Leaf"))),
		field("Second", "second", required(&typegrammar.Slice{Element: ref("Mid")})),
	)
	root.Fields[0].Description = "Ignored for $ref."
	defs := typegrammar.Definitions{def("Root", root), described(def("Leaf", leaf), "A leaf."), def("Mid", mid)}
	refs := map[typegrammar.Name]bool{name("Leaf"): true, name("Mid"): true}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{Refs: refs})
	require.Equal(t, `{"$defs":{`+
		`"Leaf":{"type":"object","description":"A leaf.","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false},`+
		`"Mid":{"type":"object","properties":{"leaf":{"$ref":"#/$defs/Leaf"}},"required":["leaf"],"additionalProperties":false}},`+
		`"type":"object","properties":{"first":{"$ref":"#/$defs/Leaf"},"second":{"type":"array","items":{"$ref":"#/$defs/Mid"}}},`+
		`"required":["first","second"],"additionalProperties":false}`, got)

	// A root in Refs renders its own body; only other definitions refer to it.
	got = generateOne(t, defs, Root{Name: name("Leaf")}, Options{Refs: refs})
	require.Equal(t, `{"type":"object","description":"A leaf.","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`, got)
}

func TestGenerateRefNameCollision(t *testing.T) {
	other := typegrammar.Name{PackagePath: "example.com/other", Name: "Leaf"}
	leaf := object(field("Value", "value", required(scalarOf(typegrammar.String))))
	root := object(
		field("Local", "local", required(ref("Leaf"))),
		field("Remote", "remote", required(&typegrammar.Ref{Target: other})),
	)
	defs := typegrammar.Definitions{
		def("Root", root),
		def("Leaf", leaf),
		{Name: other, Type: leaf, Source: pos(9)},
	}

	_, err := Generate(defs, []Root{{Name: name("Root")}}, Options{Refs: map[typegrammar.Name]bool{name("Leaf"): true, other: true}})
	require.ErrorContains(t, err, `AsRef definition name collision: "Leaf" is used by both example.com/fixture.Leaf and example.com/other.Leaf (registered at fixture.go:9)`)
}

func TestGenerateUnions(t *testing.T) {
	circle := object(field("Radius", "radius", required(scalarOf(typegrammar.Float64))))
	square := object(field("Side", "side", &typegrammar.Optional{Type: scalarOf(typegrammar.Float64)}))
	union := typegrammar.Union{
		Interface:     name("Shape"),
		Discriminator: "kind",
		Source:        pos(4),
		Variants: []typegrammar.Variant{
			{Implementation: name("Circle"), Tag: "Circle", Source: pos(5)},
			{Implementation: name("Square"), Pointer: true, Tag: "sq", Source: pos(6)},
		},
	}
	root := object(
		field("Shape", "shape", &union),
		field("Maybe", "maybe", &typegrammar.OptionalUnion{Union: union}),
		field("All", "all", &typegrammar.UnionSlice{Union: union}),
	)
	root.Fields[2].Description = "Every shape."
	defs := typegrammar.Definitions{def("Root", root), described(def("Circle", circle), "Round."), def("Square", square)}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	circleJSON := `{"type":"object","description":"Round.","properties":{"kind":{"type":"string","const":"Circle"},"radius":{"type":"number"}},"required":["kind","radius"],"additionalProperties":false}`
	squareJSON := `{"type":"object","properties":{"kind":{"type":"string","const":"sq"},"side":{"type":"number"}},"required":["kind"],"additionalProperties":false}`
	require.Contains(t, got, `"shape":{"anyOf":[`+circleJSON+`,`+squareJSON+`]}`)
	require.Contains(t, got, `"maybe":{"anyOf":[`+circleJSON)
	require.Contains(t, got, `"all":{"type":"array","description":"Every shape.","items":{"anyOf":[`+circleJSON)
	require.Contains(t, got, `"required":["shape","all"]`)

	// A sealed interface root renders as the union itself.
	got = generateOne(t, defs, Root{Union: &union}, Options{})
	require.Equal(t, `{"anyOf":[`+circleJSON+`,`+squareJSON+`]}`, got)
	require.Equal(t, "Shape", Root{Union: &union}.TypeName())
}

func TestGenerateUnionDiscriminatorCollision(t *testing.T) {
	variant := object(field("Kind", "kind", required(scalarOf(typegrammar.String))))
	union := typegrammar.Union{Interface: name("Shape"), Discriminator: "kind", Source: pos(4),
		Variants: []typegrammar.Variant{{Implementation: name("Circle"), Tag: "Circle", Source: pos(5)}}}
	defs := typegrammar.Definitions{
		def("Root", object(field("Shape", "shape", &union))),
		{Name: name("Circle"), Type: variant, Source: pos(7)},
	}

	_, err := Generate(defs, []Root{{Name: name("Root")}}, Options{})
	require.EqualError(t, err, `variant Circle of sealed interface Shape has a payload property "kind" that collides with the discriminator property at fixture.go:7`)
}

func TestGenerateRecursionErrors(t *testing.T) {
	t.Run("self reference", func(t *testing.T) {
		defs := typegrammar.Definitions{
			{Name: name("Node"), Source: pos(3), Type: object(field("Next", "next", required(&typegrammar.Pointer{Element: ref("Node")})))},
		}
		_, err := Generate(defs, []Root{{Name: name("Node")}}, Options{})
		var recursion *RecursionError
		require.ErrorAs(t, err, &recursion)
		require.Equal(t, name("Node"), recursion.Type)
		require.Equal(t, pos(3), recursion.Source)
		require.Equal(t, "Node", recursion.Root.TypeName())
		require.EqualError(t, err, "JSON Schema cannot express the recursive type example.com/fixture.Node (fixture.go:3), reached from root Node")
	})

	t.Run("mutual reference through a collection", func(t *testing.T) {
		defs := typegrammar.Definitions{
			def("A", object(field("Bs", "bs", required(&typegrammar.Slice{Element: ref("B")})))),
			def("B", object(field("A", "a", &typegrammar.Optional{Type: ref("A")}))),
		}
		_, err := Generate(defs, []Root{{Name: name("A")}}, Options{})
		var recursion *RecursionError
		require.ErrorAs(t, err, &recursion)
		require.Equal(t, name("A"), recursion.Type)
	})

	t.Run("union reaching its own interface", func(t *testing.T) {
		union := typegrammar.Union{Interface: name("Block"), Discriminator: "type", Source: pos(8),
			Variants: []typegrammar.Variant{{Implementation: name("Group"), Tag: "Group", Source: pos(9)}}}
		defs := typegrammar.Definitions{
			def("Root", object(field("Block", "block", &union))),
			def("Group", object(field("Children", "children", &typegrammar.UnionSlice{Union: union}))),
		}
		_, err := Generate(defs, []Root{{Name: name("Root")}}, Options{})
		var recursion *RecursionError
		require.ErrorAs(t, err, &recursion)
		require.Equal(t, name("Block"), recursion.Type)
		require.Equal(t, pos(8), recursion.Source)
	})

	t.Run("shared but not recursive", func(t *testing.T) {
		leaf := def("Leaf", object(field("Value", "value", required(scalarOf(typegrammar.String)))))
		defs := typegrammar.Definitions{
			def("Root", object(field("A", "a", required(ref("Leaf"))), field("B", "b", required(ref("Leaf"))))),
			leaf,
		}
		_, err := Generate(defs, []Root{{Name: name("Root")}}, Options{})
		require.NoError(t, err)
	})
}

func TestGenerateProvidedFields(t *testing.T) {
	root := object(
		field("Hole", "hole", &typegrammar.Provided{}),
		field("Ref", "ref", &typegrammar.Provided{Ref: "https://example.test/schema.json"}),
		field("Maybe", "maybe", &typegrammar.Provided{Optional: true}),
	)
	defs := typegrammar.Definitions{def("Root", root)}

	got := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	require.Equal(t, `{"type":"object","properties":{"hole":{{.hole}},"ref":{"$ref":"https://example.test/schema.json"},"maybe":{{.maybe}}},"required":["hole","ref"],"additionalProperties":false}`, got)
}

func TestGenerateRejectsInvalidDefinitions(t *testing.T) {
	defs := typegrammar.Definitions{
		def("Root", object(field("Data", "data", required(&typegrammar.Slice{Element: scalarOf(typegrammar.Uint8)})))),
	}
	_, err := Generate(defs, []Root{{Name: name("Root")}}, Options{})
	require.ErrorContains(t, err, "generate JSON Schema: ")
	require.ErrorContains(t, err, "byte-like slices")

	_, err = Generate(typegrammar.Definitions{}, []Root{{Name: name("Missing")}}, Options{})
	require.EqualError(t, err, "generate JSON Schema: unresolved definition example.com/fixture.Missing")
	require.False(t, errors.As(err, new(*RecursionError)))
}

func TestGenerateDoesNotMutateDefinitions(t *testing.T) {
	leaf := object(field("Value", "value", required(scalarOf(typegrammar.String))))
	root := object(field("Leaf", "leaf", required(ref("Leaf"))))
	root.Fields[0].Description = "override"
	defs := typegrammar.Definitions{def("Root", root), described(def("Leaf", leaf), "original")}

	first := generateOne(t, defs, Root{Name: name("Root")}, Options{})
	require.Contains(t, first, `"description":"override"`)
	require.Equal(t, "original", defs[1].Description)
	require.Equal(t, first, generateOne(t, defs, Root{Name: name("Root")}, Options{}))
}
