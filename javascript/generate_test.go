package javascript

import (
	"go/constant"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
	"github.com/tylergannon/polytype/typescript"
)

const testPackage = "example.com/model"

func grammarName(name string) typegrammar.Name {
	return typegrammar.Name{PackagePath: testPackage, Name: name}
}

func definition(name string, typ typegrammar.Type) typegrammar.Definition {
	return typegrammar.Definition{Name: grammarName(name), Type: typ}
}

func required(goName, jsonName string, typ typegrammar.Type) typegrammar.Field {
	return typegrammar.Field{GoName: goName, JSONName: jsonName, Value: &typegrammar.Required{Type: typ}}
}

func integer(value string) constant.Value {
	return constant.MakeFromLiteral(value, token.INT, 0)
}

// completeGrammarDefinitions is the same graph the TypeScript package's
// TestGenerateProjectsCompleteGrammar accepts: every ordinary constructor, an
// enum in value and name mode, nullable/optional fields, and a discriminated
// union used directly, optionally, and as a slice.
func completeGrammarDefinitions() typegrammar.Definitions {
	created := typegrammar.Definition{
		Name:        grammarName("Created"),
		Description: "Created payload. */ still documented.\r\nSecond line.",
		Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("ID", "id", &typegrammar.Scalar{Kind: typegrammar.String}),
			{
				GoName:      "Kind",
				JSONName:    "kind-key",
				Description: "A tag with\u0000 control.",
				Value:       &typegrammar.Optional{Type: &typegrammar.Scalar{Kind: typegrammar.String}},
			},
		}},
	}
	deleted := definition("Deleted", &typegrammar.Object{})
	status := definition("Status", &typegrammar.Enum{
		GoType: grammarName("Status"),
		Kind:   typegrammar.String,
		Mode:   typegrammar.EnumValues,
		Members: []typegrammar.EnumMember{
			{Name: "Ready", Value: constant.MakeString("ready")},
			{Name: "Quoted", Value: constant.MakeString("quote\"slash\\\n雪")},
		},
	})
	mode := definition("Mode", &typegrammar.Enum{
		GoType: grammarName("Mode"),
		Kind:   typegrammar.Int8,
		Mode:   typegrammar.EnumNames,
		Members: []typegrammar.EnumMember{
			{Name: "Fast", Value: integer("0")},
			{Name: "Safe", Value: integer("1")},
		},
	})
	exact := definition("Exact", &typegrammar.Enum{
		GoType: grammarName("Exact"),
		Kind:   typegrammar.Int64,
		Mode:   typegrammar.EnumValues,
		Members: []typegrammar.EnumMember{
			{Name: "Negative", Value: integer("-1")},
			{Name: "Large", Value: integer("9007199254740992")},
		},
	})
	u := typegrammar.Union{
		Interface:     grammarName("Event"),
		Discriminator: "kind-key",
		Variants: []typegrammar.Variant{
			{Implementation: grammarName("Created"), Tag: "created"},
			{Implementation: grammarName("Deleted"), Pointer: true, Tag: ""},
		},
	}
	owner := definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{
		required("Count", "count", &typegrammar.Scalar{Kind: typegrammar.Uint64}),
		required("When", "when", &typegrammar.Time{}),
		{GoName: "Status", JSONName: "status", Value: &typegrammar.Nullable{Type: &typegrammar.Ref{Target: grammarName("Status")}}},
		{GoName: "Mode", JSONName: "mode", Value: &typegrammar.Optional{Type: &typegrammar.Ref{Target: grammarName("Mode")}}},
		{GoName: "Values", JSONName: "values", Value: &typegrammar.Optional{Type: &typegrammar.Array{Length: 2, Element: &typegrammar.Pointer{Element: &typegrammar.Scalar{Kind: typegrammar.Bool}}}}},
		{GoName: "Event", JSONName: "event", Value: &u},
		{GoName: "MaybeEvent", JSONName: "maybe-event", Value: &typegrammar.OptionalUnion{Union: u}},
		{GoName: "Events", JSONName: "events", Value: &typegrammar.UnionSlice{Union: u}},
	}})
	return typegrammar.Definitions{created, deleted, status, mode, exact, owner}
}

func recursiveDefinitions() typegrammar.Definitions {
	node := grammarName("Node")
	return typegrammar.Definitions{{
		Name: node,
		Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("Value", "value", &typegrammar.Scalar{Kind: typegrammar.String}),
			{GoName: "Next", JSONName: "next", Value: &typegrammar.Optional{Type: &typegrammar.Ref{Target: node}}},
		}},
	}}
}

func mutuallyRecursiveDefinitions() typegrammar.Definitions {
	a := grammarName("A")
	b := grammarName("B")
	return typegrammar.Definitions{
		{Name: a, Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("B", "b", &typegrammar.Ref{Target: b}),
		}}},
		{Name: b, Type: &typegrammar.Object{Fields: []typegrammar.Field{
			{GoName: "A", JSONName: "a", Value: &typegrammar.Optional{Type: &typegrammar.Ref{Target: a}}},
		}}},
	}
}

// edgeDefinitions mirrors the TypeScript output-tools edge case: a heavily
// escaped discriminator and property name that JSDoc cannot spell as a
// @property name, plus Unicode and literal names that collide.
func edgeDefinitions() typegrammar.Definitions {
	discriminator := "kind\"\\\n雪"
	payload := typegrammar.Definition{
		Name:        grammarName("Payload"),
		Description: "Payload comment closes */ then continues.\u2028Next line separator.",
		Type: &typegrammar.Object{Fields: []typegrammar.Field{
			{GoName: "Kind", JSONName: discriminator, Value: &typegrammar.Optional{Type: &typegrammar.Scalar{Kind: typegrammar.String}}},
			required("Value", "value", &typegrammar.Scalar{Kind: typegrammar.String}),
		}},
	}
	owner := definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{{
		GoName:   "Event",
		JSONName: "event",
		Value: &typegrammar.Union{
			Interface:     grammarName("Event"),
			Discriminator: discriminator,
			Variants:      []typegrammar.Variant{{Implementation: grammarName("Payload")}},
		},
	}}})
	return typegrammar.Definitions{
		payload,
		owner,
		definition("雪", &typegrammar.Object{}),
		definition("_u96EA_", &typegrammar.Object{}),
	}
}

func collisionDefinitions() typegrammar.Definitions {
	left := typegrammar.Name{PackagePath: "example.com/left", Name: "Shared"}
	right := typegrammar.Name{PackagePath: "example.com/right", Name: "Shared"}
	return typegrammar.Definitions{
		{Name: left, Type: &typegrammar.Object{}},
		{Name: right, Type: &typegrammar.Object{}},
		definition("Array", &typegrammar.Object{}),
		definition("Omit", &typegrammar.Object{}),
		definition("object", &typegrammar.Object{}),
		definition("雪", &typegrammar.Object{}),
		definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{
			required("Left", "left", &typegrammar.Ref{Target: left}),
			required("Right", "right", &typegrammar.Ref{Target: right}),
		}}),
	}
}

// acceptedDefinitionSets is the cross-check corpus: every graph the TypeScript
// package's tests accept must also be accepted by this backend, and both must
// allocate the same typedef identifiers.
func acceptedDefinitionSets() map[string]func() typegrammar.Definitions {
	return map[string]func() typegrammar.Definitions{
		"complete grammar":   completeGrammarDefinitions,
		"recursive":          recursiveDefinitions,
		"mutually recursive": mutuallyRecursiveDefinitions,
		"edge escaping":      edgeDefinitions,
		"collisions":         collisionDefinitions,
		"empty":              func() typegrammar.Definitions { return nil },
	}
}

func TestGenerateProjectsCompleteGrammar(t *testing.T) {
	t.Parallel()

	result, err := Generate(completeGrammarDefinitions(), Options{})
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	require.Equal(t, "types.js", result.Files[0].Name)

	js := string(result.Files[0].Content)
	require.True(t, strings.HasPrefix(js, GeneratedHeader))
	require.Contains(t, js, " * Created payload. *\\/ still documented.")
	require.Contains(t, js, " * Second line.")
	require.Contains(t, js, " * @typedef {Object} Created")
	require.Contains(t, js, " * @property {string} id")
	require.Contains(t, js, " * @property {string} [kind-key] A tag with\\u0000 control.")
	require.Contains(t, js, " * @typedef {object} Deleted")
	require.Contains(t, js, ` * @typedef {"ready" | "quote\"slash\\\n雪"} Status`)
	require.Contains(t, js, ` * @typedef {"Fast" | "Safe"} Mode`)
	require.Contains(t, js, " * @typedef {-1 | 9007199254740992} Exact")
	require.Contains(t, js, " * @property {number} count")
	require.Contains(t, js, " * @property {string} when")
	require.Contains(t, js, " * @property {Status | null} status")
	require.Contains(t, js, " * @property {Mode} [mode]")
	require.Contains(t, js, " * @property {Array<boolean>} [values]")
	require.Contains(t, js, ` * @property {Omit<Created, "kind-key"> & {"kind-key": "created"} | Omit<Deleted, "kind-key"> & {"kind-key": ""}} event`)
	require.Contains(t, js, ` * @property {Omit<Created, "kind-key"> & {"kind-key": "created"} | Omit<Deleted, "kind-key"> & {"kind-key": ""}} [maybe-event]`)
	require.Contains(t, js, ` * @property {Array<Omit<Created, "kind-key"> & {"kind-key": "created"} | Omit<Deleted, "kind-key"> & {"kind-key": ""}>} events`)
	require.False(t, strings.Contains(js, " any"))
	require.False(t, strings.Contains(js, " unknown"))
	require.False(t, strings.Contains(js, "export type"))

	require.Equal(t, []string{"export {};"}, exportStatements(js))
}

func TestGenerateWritesExactSimpleModule(t *testing.T) {
	t.Parallel()

	defs := typegrammar.Definitions{{
		Name:        grammarName("Person"),
		Description: "A person.",
		Type: &typegrammar.Object{Fields: []typegrammar.Field{
			required("Name", "name", &typegrammar.Scalar{Kind: typegrammar.String}),
		}},
	}}
	result, err := Generate(defs, Options{})
	require.NoError(t, err)
	require.Equal(t, GeneratedHeader+`
/**
 * A person.
 * @typedef {Object} Person
 * @property {string} name
 */

export {};
`, string(result.Files[0].Content))
}

func TestGenerateRendersMultiLinePropertyDescription(t *testing.T) {
	t.Parallel()

	defs := typegrammar.Definitions{{
		Name: grammarName("Doc"),
		Type: &typegrammar.Object{Fields: []typegrammar.Field{{
			GoName:      "Name",
			JSONName:    "name",
			Description: "line one\nline two\u0001",
			Value:       &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}},
		}}},
	}}
	result, err := Generate(defs, Options{})
	require.NoError(t, err)
	require.Contains(t, string(result.Files[0].Content), " * @property {string} name line one\n * line two\\u0001\n")
}

func TestGenerateRecursiveRefs(t *testing.T) {
	t.Parallel()

	result, err := Generate(recursiveDefinitions(), Options{})
	require.NoError(t, err)
	js := string(result.Files[0].Content)
	require.Contains(t, js, " * @typedef {Object} Node")
	require.Contains(t, js, " * @property {Node} [next]")

	// A self-referential typedef still imports and type-checks structurally.
	require.Equal(t, map[typegrammar.Name]string{grammarName("Node"): "Node"}, result.Names)
}

func TestGenerateInlinesObjectsWithNamesJSDocCannotSpell(t *testing.T) {
	t.Parallel()

	discriminator := "kind\"\\\n雪"
	defs := typegrammar.Definitions{
		{
			Name: grammarName("Payload"),
			Type: &typegrammar.Object{Fields: []typegrammar.Field{
				{GoName: "Kind", JSONName: discriminator, Value: &typegrammar.Optional{Type: &typegrammar.Scalar{Kind: typegrammar.String}}},
				required("Value", "value", &typegrammar.Scalar{Kind: typegrammar.String}),
			}},
		},
	}
	result, err := Generate(defs, Options{})
	require.NoError(t, err)
	js := string(result.Files[0].Content)
	require.NotContains(t, js, "@property")
	require.Contains(t, js, ` * @typedef {{"kind\"\\\n雪"?: string, "value": string}} Payload`)
}

func TestGenerateUsesStableCollisionSafeNames(t *testing.T) {
	t.Parallel()

	defs := collisionDefinitions()
	first, err := Generate(defs, Options{})
	require.NoError(t, err)
	second, err := Generate(defs, Options{})
	require.NoError(t, err)
	require.Equal(t, first, second)

	left := typegrammar.Name{PackagePath: "example.com/left", Name: "Shared"}
	right := typegrammar.Name{PackagePath: "example.com/right", Name: "Shared"}
	require.Equal(t, map[typegrammar.Name]string{
		left:                  "Shared$6578616d706c652e636f6d2f6c65667400536861726564",
		right:                 "Shared$6578616d706c652e636f6d2f726967687400536861726564",
		grammarName("object"): "object$type",
		grammarName("Array"):  "Array$type",
		grammarName("Omit"):   "Omit$type",
		grammarName("\u96ea"): "_u96EA_",
		grammarName("Owner"):  "Owner",
	}, first.Names)

	js := string(first.Files[0].Content)
	require.Contains(t, js, " * @typedef {object} Shared$6578616d706c652e636f6d2f6c65667400536861726564")
	require.Contains(t, js, " * @typedef {object} object$type")
	require.Contains(t, js, " * @typedef {object} Array$type")
	require.Contains(t, js, " * @typedef {object} Omit$type")
	require.Contains(t, js, " * @typedef {object} _u96EA_")
	require.Contains(t, js, ` * @property {Shared$6578616d706c652e636f6d2f6c65667400536861726564} left`)
	require.Contains(t, js, ` * @property {Shared$6578616d706c652e636f6d2f726967687400536861726564} right`)
}

func TestGenerateMatchesTypeScriptAcceptance(t *testing.T) {
	t.Parallel()

	for name, defsFunc := range acceptedDefinitionSets() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := defsFunc()
			typeScript, err := typescript.Generate(defs, typescript.Options{Barrel: true})
			require.NoError(t, err, "TypeScript must accept the %s set", name)

			javaScript, err := Generate(defs, Options{})
			require.NoError(t, err, "JavaScript must accept the set TypeScript accepts (%s)", name)
			require.Equal(t, typeScript.Names, javaScript.Names,
				"both backends must allocate the same typedef identifiers for %s", name)
			require.Equal(t, []string{"export {};"}, exportStatements(string(javaScript.Files[0].Content)),
				"the JavaScript module must export no runtime bindings for %s", name)
		})
	}
}

func TestGenerateRejectsProvidedField(t *testing.T) {
	t.Parallel()

	defs := typegrammar.Definitions{definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{{
		GoName:   "Outside",
		JSONName: "outside",
		Value:    &typegrammar.Provided{Ref: "External"},
	}}})}
	result, err := Generate(defs, Options{})
	require.Zero(t, result)
	require.ErrorContains(t, err, "generate JavaScript: example.com/model.Owner.Outside")
	require.ErrorContains(t, err, "provided outside the type grammar and has no static type")
}

func TestGenerateRejectsInvalidGrammarBeforeProjection(t *testing.T) {
	t.Parallel()

	result, err := Generate(typegrammar.Definitions{
		definition("Broken", &typegrammar.Ref{Target: grammarName("Missing")}),
	}, Options{})
	require.Zero(t, result)
	require.ErrorContains(t, err, "generate JavaScript: ")
	require.ErrorContains(t, err, "unresolved reference example.com/model.Missing")
}

func TestGenerateRejectsInexactNumericLiteral(t *testing.T) {
	t.Parallel()

	defs := typegrammar.Definitions{definition("Large", &typegrammar.Enum{
		GoType: grammarName("Large"),
		Kind:   typegrammar.Int64,
		Mode:   typegrammar.EnumValues,
		Members: []typegrammar.EnumMember{
			{Name: "NotExact", Value: integer("9007199254740993")},
		},
	})}
	result, err := Generate(defs, Options{})
	require.Zero(t, result)
	require.ErrorContains(t, err, "example.com/model.Large enum member NotExact")
	require.ErrorContains(t, err, "9007199254740993 is not exactly representable")
}

func TestGenerateEmptyDefinitions(t *testing.T) {
	t.Parallel()

	result, err := Generate(nil, Options{})
	require.NoError(t, err)
	require.Equal(t, []File{{Name: "types.js", Content: []byte(GeneratedHeader + "export {};\n")}}, result.Files)
	require.Empty(t, result.Names)
}

func TestPrinterHonorsUnionAndIntersectionPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr typeExpr
		want string
	}{
		{
			name: "union within intersection",
			expr: intersection(
				union(keywordType("string"), keywordType("null")),
				referenceType("Tagged"),
			),
			want: `(string | null) & Tagged`,
		},
		{
			name: "generic argument owns its expression",
			expr: genericType{name: "Array", arguments: []typeExpr{
				union(literalType(`"a"`), literalType(`"b"`)),
			}},
			want: `Array<"a" | "b">`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			writeType(&out, tc.expr, 0)
			require.Equal(t, tc.want, out.String())
		})
	}
}

// exportStatements returns the lines that begin an ES export statement. The
// generated module's only legal export is the empty `export {};`; a typedef
// lives inside a block comment and starts with ` *`.
func exportStatements(js string) []string {
	var exports []string
	for line := range strings.SplitSeq(js, "\n") {
		if strings.HasPrefix(line, "export") {
			exports = append(exports, line)
		}
	}
	return exports
}
