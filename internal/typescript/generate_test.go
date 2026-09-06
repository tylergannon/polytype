package typescript

import (
	"go/constant"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
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

func TestGenerateProjectsCompleteGrammar(t *testing.T) {
	t.Parallel()

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

	files, err := Generate(typegrammar.Definitions{created, deleted, status, mode, exact, owner}, Options{Barrel: true})
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, "types.ts", files[0].Name)
	require.Equal(t, "index.ts", files[1].Name)

	types := string(files[0].Content)
	require.True(t, strings.HasPrefix(types, GeneratedHeader))
	require.Contains(t, types, "export type Created = {\n")
	require.Contains(t, types, "Created payload. *\\/ still documented.")
	require.Contains(t, types, " * Second line.")
	require.Contains(t, types, `"kind-key"?: string;`)
	require.Contains(t, types, `A tag with\u0000 control.`)
	require.Contains(t, types, "export type Deleted = object;")
	require.Contains(t, types, `export type Status = "ready" | "quote\"slash\\\n雪";`)
	require.Contains(t, types, `export type Mode = "Fast" | "Safe";`)
	require.Contains(t, types, `export type Exact = -1 | 9007199254740992;`)
	require.Contains(t, types, `"status": Status | null;`)
	require.Contains(t, types, `"mode"?: Mode;`)
	require.Contains(t, types, `"values"?: Array<boolean>;`)
	require.Contains(t, types, `Omit<Created, "kind-key"> & {`)
	require.Contains(t, types, `"kind-key": "created";`)
	require.Contains(t, types, `Omit<Deleted, "kind-key"> & {`)
	require.Contains(t, types, `"kind-key": "";`)
	require.Contains(t, types, `"maybe-event"?: Omit<Created`)
	require.Contains(t, types, `"events": Array<Omit<Created`)
	require.NotContains(t, types, " any")
	require.NotContains(t, types, " unknown")
	require.NotContains(t, types, " = {}")

	barrel := string(files[1].Content)
	require.Equal(t, GeneratedHeader+"\nexport type {\n  Created,\n  Deleted,\n  Exact,\n  Mode,\n  Owner,\n  Status,\n} from './types.js';\n", barrel)
}

func TestGenerateUsesStableCollisionSafeNames(t *testing.T) {
	t.Parallel()

	left := typegrammar.Name{PackagePath: "example.com/left", Name: "Shared"}
	right := typegrammar.Name{PackagePath: "example.com/right", Name: "Shared"}
	defs := typegrammar.Definitions{
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

	first, err := Generate(defs, Options{Barrel: true})
	require.NoError(t, err)
	second, err := Generate(defs, Options{Barrel: true})
	require.NoError(t, err)
	require.Equal(t, first, second)

	types := string(first[0].Content)
	require.Contains(t, types, "export type Shared$6578616d706c652e636f6d2f6c65667400536861726564 = object;")
	require.Contains(t, types, "export type Shared$6578616d706c652e636f6d2f726967687400536861726564 = object;")
	require.Contains(t, types, "export type object$type = object;")
	require.Contains(t, types, "export type Array$type = object;")
	require.Contains(t, types, "export type Omit$type = object;")
	require.Contains(t, types, "export type _u96EA_ = object;")
	require.Contains(t, types, `"left": Shared$6578616d706c652e636f6d2f6c65667400536861726564;`)
	require.Contains(t, types, `"right": Shared$6578616d706c652e636f6d2f726967687400536861726564;`)
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
	files, err := Generate(defs, Options{})
	require.Nil(t, files)
	require.ErrorContains(t, err, "example.com/model.Large enum member NotExact")
	require.ErrorContains(t, err, "9007199254740993 is not exactly representable")
}

func TestGenerateRejectsInvalidGrammarBeforeProjection(t *testing.T) {
	t.Parallel()

	files, err := Generate(typegrammar.Definitions{definition("Broken", &typegrammar.Ref{Target: grammarName("Missing")})}, Options{})
	require.Nil(t, files)
	require.ErrorContains(t, err, "unresolved reference example.com/model.Missing")
}

func TestGenerateEmptyDefinitionsAndOptionalBarrel(t *testing.T) {
	t.Parallel()

	without, err := Generate(nil, Options{})
	require.NoError(t, err)
	require.Equal(t, []File{{Name: "types.ts", Content: []byte(GeneratedHeader + "export {};\n")}}, without)

	with, err := Generate(nil, Options{Barrel: true})
	require.NoError(t, err)
	require.Equal(t, GeneratedHeader+"\nexport type {} from './types.js';\n", string(with[1].Content))
}
