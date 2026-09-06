package typegrammar_test

import (
	"errors"
	"fmt"
	"go/constant"
	"go/token"
	"strings"
	"testing"

	g "github.com/tylergannon/polytype/typegrammar"
)

func ExampleDefinitions_Validate() {
	payload := g.Name{PackagePath: "example.com/events", Name: "Created"}
	defs := g.Definitions{
		{Name: payload, Type: &g.Object{Fields: []g.Field{{
			GoName: "Text", JSONName: "text", Value: &g.Required{Type: &g.Scalar{Kind: g.String}},
		}}}},
		{Name: g.Name{PackagePath: "example.com/events", Name: "Envelope"}, Type: &g.Object{Fields: []g.Field{{
			GoName: "Event", JSONName: "event", Value: &g.Union{
				Interface:     g.Name{PackagePath: "example.com/events", Name: "Event"},
				Discriminator: "kind",
				Variants:      []g.Variant{{Implementation: payload, Pointer: true, Tag: "created"}},
			},
		}}}},
	}
	fmt.Println(defs.Validate())
	// Output: <nil>
}

func name(local string) g.Name { return g.Name{PackagePath: "example.com/model", Name: local} }

func definition(local string, typ g.Type) g.Definition {
	return g.Definition{Name: name(local), Type: typ}
}

func required(goName, jsonName string, typ g.Type) g.Field {
	return g.Field{GoName: goName, JSONName: jsonName, Value: &g.Required{Type: typ}}
}

func integer(text string) constant.Value { return constant.MakeFromLiteral(text, token.INT, 0) }

func enum(kind g.ScalarKind, mode g.EnumMode, members ...g.EnumMember) *g.Enum {
	return &g.Enum{GoType: name("State"), Kind: kind, Mode: mode, Members: members}
}

func union(tag string) g.Union {
	return g.Union{
		Interface: name("Event"), Discriminator: "type",
		Variants: []g.Variant{{Implementation: name("Created"), Tag: tag}},
	}
}

func unionDefinitions(value g.FieldValue) g.Definitions {
	return g.Definitions{
		definition("Created", &g.Object{Fields: []g.Field{required("Text", "text", &g.Scalar{Kind: g.String})}}),
		definition("Owner", &g.Object{Fields: []g.Field{{GoName: "Event", JSONName: "event", Value: value}}}),
	}
}

func requireValid(t *testing.T, defs g.Definitions) {
	t.Helper()
	if err := defs.Validate(); err != nil {
		t.Fatalf("valid grammar rejected: %v", err)
	}
}

func requireInvalid(t *testing.T, defs g.Definitions) *g.Error {
	t.Helper()
	err := defs.Validate()
	if err == nil {
		t.Fatal("invalid grammar accepted")
	}
	var diagnostic *g.Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("validation error lacks grammar diagnostic: %T: %v", err, err)
	}
	if diagnostic.Path == "" || diagnostic.Message == "" {
		t.Fatalf("diagnostic lacks a path or message: %#v", diagnostic)
	}
	return diagnostic
}

func TestValidateScalarKinds(t *testing.T) {
	for _, kind := range []g.ScalarKind{
		g.Bool, g.String, g.Int, g.Int8, g.Int16, g.Int32, g.Int64,
		g.Uint, g.Uint8, g.Uint16, g.Uint32, g.Uint64, g.Float32, g.Float64,
	} {
		t.Run(string(kind), func(t *testing.T) {
			requireValid(t, g.Definitions{definition("Value", &g.Scalar{Kind: kind})})
		})
	}
	for _, kind := range []g.ScalarKind{"", "complex128", "uintptr", "number"} {
		t.Run("reject_"+string(kind), func(t *testing.T) {
			_ = requireInvalid(t, g.Definitions{definition("Value", &g.Scalar{Kind: kind})})
		})
	}
}

func TestValidateSharedAcyclicGrammar(t *testing.T) {
	// Shared nodes and named references describe reuse, not recursion. A named
	// object's own union field remains valid when the object is in a collection.
	shared := &g.Object{Fields: []g.Field{required("Value", "value", &g.Scalar{Kind: g.Int64})}}
	defs := unionDefinitions(&g.UnionSlice{Union: union("created")})
	defs = append(defs,
		definition("Shared", shared),
		definition("SharedAgain", shared),
		definition("Envelope", &g.Object{Fields: []g.Field{
			required("Left", "left", &g.Ref{Target: name("Shared")}),
			required("Right", "right", &g.Ref{Target: name("Shared")}),
			required("Owners", "owners", &g.Slice{Element: &g.Ref{Target: name("Owner")}}),
			required("EmptyArray", "emptyArray", &g.Array{Length: 0, Element: &g.Scalar{Kind: g.String}}),
			required("Array", "array", &g.Array{Length: 3, Element: &g.Pointer{Element: &g.Ref{Target: name("Shared")}}}),
		}}),
	)
	requireValid(t, defs)
	requireValid(t, nil)
}

func TestValidateRequiredOptionalAndNullable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value g.FieldValue
	}{
		{"required_scalar", &g.Required{Type: &g.Scalar{Kind: g.String}}},
		{"required_pointer_scalar", &g.Required{Type: &g.Pointer{Element: &g.Scalar{Kind: g.Int}}}},
		{"optional_scalar", &g.Optional{Type: &g.Scalar{Kind: g.Bool}}},
		{"optional_array", &g.Optional{Type: &g.Array{Length: 2, Element: &g.Scalar{Kind: g.Int}}}},
		{"optional_slice", &g.Optional{Type: &g.Slice{Element: &g.Scalar{Kind: g.String}}}},
		{"optional_pointer", &g.Optional{Type: &g.Pointer{Element: &g.Ref{Target: name("Payload")}}}},
		{"nullable_scalar", &g.Nullable{Type: &g.Scalar{Kind: g.String}}},
		{"nullable_object", &g.Nullable{Type: &g.Object{}}},
		{"nullable_ref", &g.Nullable{Type: &g.Ref{Target: name("Payload")}}},
		{"nullable_pointer_object", &g.Nullable{Type: &g.Pointer{Element: &g.Object{}}}},
		{"nullable_pointer_ref", &g.Nullable{Type: &g.Pointer{Element: &g.Ref{Target: name("Payload")}}}},
		{"required_time", &g.Required{Type: &g.Time{}}},
		{"optional_time", &g.Optional{Type: &g.Time{}}},
		{"nullable_time", &g.Nullable{Type: &g.Time{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireValid(t, g.Definitions{
				definition("Payload", &g.Object{}),
				definition("Owner", &g.Object{Fields: []g.Field{{GoName: "Value", JSONName: "value", Value: tc.value}}}),
			})
		})
	}
	for _, tc := range []struct {
		name string
		typ  g.Type
	}{
		{"array", &g.Array{Length: 2, Element: &g.Scalar{Kind: g.Int}}},
		{"slice", &g.Slice{Element: &g.Scalar{Kind: g.String}}},
		{"pointer_scalar", &g.Pointer{Element: &g.Scalar{Kind: g.String}}},
		{"double_pointer_object", &g.Pointer{Element: &g.Pointer{Element: &g.Object{}}}},
		{"ref_to_slice", &g.Ref{Target: name("Strings")}},
	} {
		t.Run("reject_nullable_"+tc.name, func(t *testing.T) {
			_ = requireInvalid(t, g.Definitions{
				definition("Strings", &g.Slice{Element: &g.Scalar{Kind: g.String}}),
				definition("Owner", &g.Object{Fields: []g.Field{{GoName: "Value", JSONName: "value", Value: &g.Nullable{Type: tc.typ}}}}),
			})
		})
	}
}

func TestValidateUnionFormsAndFieldLocalIdentity(t *testing.T) {
	for _, tag := range []string{"created", "", " ", "quote\"slash\\\n雪"} {
		for _, form := range []string{"required", "optional", "slice"} {
			t.Run(form+"/"+tag, func(t *testing.T) {
				u := union(tag)
				var field g.FieldValue = &u
				switch form {
				case "optional":
					field = &g.OptionalUnion{Union: u}
				case "slice":
					field = &g.UnionSlice{Union: u}
				}
				requireValid(t, unionDefinitions(field))
			})
		}
	}

	first := union("created")
	second := union("new")
	second.Discriminator = "!kind"
	second.Variants[0].Pointer = true
	defs := unionDefinitions(&first)
	owner := defs[1].Type.(*g.Object)
	owner.Fields = append(owner.Fields, g.Field{GoName: "Other", JSONName: "other", Value: &second})
	requireValid(t, defs)
	if first.Discriminator != "type" || first.Variants[0].Tag != "created" || first.Variants[0].Pointer {
		t.Fatal("validation changed the first field's union registration")
	}
	if second.Discriminator != "!kind" || second.Variants[0].Tag != "new" || !second.Variants[0].Pointer {
		t.Fatal("validation changed the second field's union registration")
	}
}

func TestValidateRejectsInvalidUnions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*g.Union)
	}{
		{"missing_discriminator", func(u *g.Union) { u.Discriminator = "" }},
		{"missing_interface", func(u *g.Union) { u.Interface = g.Name{} }},
		{"no_variants", func(u *g.Union) { u.Variants = nil }},
		{"duplicate_tag", func(u *g.Union) {
			u.Variants = append(u.Variants, g.Variant{Implementation: name("Deleted"), Tag: u.Variants[0].Tag})
		}},
		{"missing_implementation", func(u *g.Union) { u.Variants[0].Implementation = name("Missing") }},
		{"scalar_implementation", func(u *g.Union) { u.Variants[0].Implementation = name("Scalar") }},
		{"duplicate_implementation", func(u *g.Union) {
			u.Variants = append(u.Variants, g.Variant{Implementation: name("Created"), Tag: "another"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := union("created")
			tc.change(&u)
			defs := unionDefinitions(&u)
			defs = append(defs, definition("Deleted", &g.Object{}), definition("Scalar", &g.Scalar{Kind: g.Int}))
			_ = requireInvalid(t, defs)
		})
	}

	t.Run("inline_owner", func(t *testing.T) {
		u := union("created")
		defs := unionDefinitions(&u)
		inline := defs[1].Type
		defs[1].Type = &g.Object{Fields: []g.Field{required("Inline", "inline", inline)}}
		_ = requireInvalid(t, defs)
	})
}

func TestValidateValueAndPointerUnionImplementations(t *testing.T) {
	u := union("value")
	u.Variants = append(u.Variants, g.Variant{Implementation: name("Created"), Pointer: true, Tag: "pointer"})
	requireValid(t, unionDefinitions(&u))
}

func TestValidateUnionDiscriminatorPayloadCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value g.FieldValue
		valid bool
	}{
		{"required_string", &g.Required{Type: &g.Scalar{Kind: g.String}}, true},
		{"optional_string", &g.Optional{Type: &g.Scalar{Kind: g.String}}, true},
		{"nullable_string", &g.Nullable{Type: &g.Scalar{Kind: g.String}}, true},
		{"matching_enum", &g.Required{Type: enum(g.String, g.EnumValues, g.EnumMember{Name: "Created", Value: constant.MakeString("created")})}, true},
		{"wrong_enum", &g.Required{Type: enum(g.String, g.EnumValues, g.EnumMember{Name: "Deleted", Value: constant.MakeString("deleted")})}, false},
		{"number", &g.Required{Type: &g.Scalar{Kind: g.Int}}, false},
		{"object", &g.Required{Type: &g.Object{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := union("created")
			defs := unionDefinitions(&u)
			payload := defs[0].Type.(*g.Object)
			payload.Fields = append(payload.Fields, g.Field{GoName: "Type", JSONName: "type", Value: tc.value})
			if tc.valid {
				requireValid(t, defs)
			} else {
				_ = requireInvalid(t, defs)
			}
			// The union branch's required constant must be a projection of its
			// registration; validation cannot rewrite a shared payload definition.
			if payload.Fields[1].Value != tc.value {
				t.Fatal("validation rewrote the payload discriminator field")
			}
		})
	}
}

func TestValidateRejectsCycles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		graph func() g.Definitions
	}{
		{"direct_ref", func() g.Definitions { return g.Definitions{definition("A", &g.Ref{Target: name("A")})} }},
		{"indirect_ref", func() g.Definitions {
			return g.Definitions{definition("A", &g.Ref{Target: name("B")}), definition("B", &g.Slice{Element: &g.Ref{Target: name("A")}})}
		}},
		{"pointer_node", func() g.Definitions {
			p := &g.Pointer{}
			p.Element = p
			return g.Definitions{definition("A", p)}
		}},
		{"object_node", func() g.Definitions {
			a, b := &g.Object{}, &g.Object{}
			a.Fields = []g.Field{required("B", "b", b)}
			b.Fields = []g.Field{required("A", "a", a)}
			return g.Definitions{definition("A", a)}
		}},
		{"union_implementation", func() g.Definitions {
			u := union("owner")
			u.Variants[0].Implementation = name("Owner")
			return unionDefinitions(&u)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { _ = requireInvalid(t, tc.graph()) })
	}
}

func TestValidateRejectsNilNodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  g.Type
	}{
		{"interface", nil}, {"scalar", (*g.Scalar)(nil)}, {"time", (*g.Time)(nil)},
		{"enum", (*g.Enum)(nil)}, {"object", (*g.Object)(nil)}, {"pointer", (*g.Pointer)(nil)},
		{"slice", (*g.Slice)(nil)}, {"array", (*g.Array)(nil)}, {"ref", (*g.Ref)(nil)},
		{"pointer_element", &g.Pointer{}}, {"slice_element", &g.Slice{}}, {"array_element", &g.Array{}},
	} {
		t.Run(tc.name, func(t *testing.T) { _ = requireInvalid(t, g.Definitions{definition("Value", tc.typ)}) })
	}
	for _, tc := range []struct {
		name  string
		value g.FieldValue
	}{
		{"interface", nil}, {"required", (*g.Required)(nil)}, {"optional", (*g.Optional)(nil)},
		{"nullable", (*g.Nullable)(nil)}, {"union", (*g.Union)(nil)},
		{"optional_union", (*g.OptionalUnion)(nil)}, {"union_slice", (*g.UnionSlice)(nil)},
		{"required_type", &g.Required{}}, {"optional_type", &g.Optional{}}, {"nullable_type", &g.Nullable{}},
	} {
		t.Run("field_"+tc.name, func(t *testing.T) {
			_ = requireInvalid(t, g.Definitions{definition("Owner", &g.Object{Fields: []g.Field{{GoName: "Value", JSONName: "value", Value: tc.value}}})})
		})
	}
}

// Embedding exports the marker method along with its node. Validation is still
// the closed grammar boundary, including for noncomparable foreign values.
type foreignType struct {
	*g.Scalar
	Extra []string
}
type foreignField struct{ *g.Required }

func TestValidateRejectsConstructorsAddedByEmbedding(t *testing.T) {
	_ = requireInvalid(t, g.Definitions{definition("Foreign", foreignType{Scalar: &g.Scalar{Kind: g.String}})})
	_ = requireInvalid(t, g.Definitions{definition("Owner", &g.Object{Fields: []g.Field{{
		GoName: "Value", JSONName: "value", Value: &foreignField{Required: &g.Required{Type: &g.Scalar{Kind: g.String}}},
	}}})})
}

func TestValidateDefinitionAndFieldNames(t *testing.T) {
	t.Run("missing_reference", func(t *testing.T) {
		_ = requireInvalid(t, g.Definitions{definition("Value", &g.Ref{Target: name("Missing")})})
	})
	t.Run("duplicate_definition", func(t *testing.T) {
		_ = requireInvalid(t, g.Definitions{definition("Value", &g.Object{}), definition("Value", &g.Object{})})
	})
	for _, invalidName := range []g.Name{{}, {Name: "Value"}, {PackagePath: "example.com/model"}, name("bad-name"), name("type")} {
		t.Run("invalid_"+invalidName.String(), func(t *testing.T) {
			_ = requireInvalid(t, g.Definitions{{Name: invalidName, Type: &g.Object{}}})
		})
	}
	for _, tc := range []struct{ name, goName, jsonName string }{
		{"duplicate_go", "First", "second"}, {"duplicate_json", "Second", "first"},
		{"invalid_go", "bad-name", "second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = requireInvalid(t, g.Definitions{definition("Owner", &g.Object{Fields: []g.Field{
				required("First", "first", &g.Scalar{Kind: g.Bool}),
				required(tc.goName, tc.jsonName, &g.Scalar{Kind: g.Bool}),
			}})})
		})
	}
	t.Run("arbitrary_json_keys", func(t *testing.T) {
		requireValid(t, g.Definitions{definition("Owner", &g.Object{Fields: []g.Field{
			required("Empty", "", &g.Scalar{Kind: g.String}),
			required("Escaped", "雪\"\\\n", &g.Scalar{Kind: g.String}),
		}})})
	})
}

func TestValidateByteLikeSlicesAndArrayLengths(t *testing.T) {
	for _, typ := range []g.Type{
		&g.Slice{Element: &g.Scalar{Kind: g.Uint8}},
		&g.Slice{Element: &g.Ref{Target: name("Byte")}},
		&g.Slice{Element: &g.Ref{Target: name("ByteAlias")}},
		&g.Slice{Element: enum(g.Uint8, g.EnumValues, g.EnumMember{Name: "Zero", Value: integer("0")})},
	} {
		_ = requireInvalid(t, g.Definitions{
			definition("Byte", &g.Scalar{Kind: g.Uint8}),
			definition("ByteAlias", &g.Ref{Target: name("Byte")}),
			definition("Bytes", typ),
		})
	}
	// Byte-valued arrays and pointers in slices use JSON arrays, not base64.
	requireValid(t, g.Definitions{
		definition("Bytes", &g.Array{Length: 8, Element: &g.Scalar{Kind: g.Uint8}}),
		definition("BytePointers", &g.Slice{Element: &g.Pointer{Element: &g.Scalar{Kind: g.Uint8}}}),
	})
	_ = requireInvalid(t, g.Definitions{definition("Array", &g.Array{Length: -1, Element: &g.Scalar{Kind: g.String}})})
}

func TestValidateExactEnums(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  *g.Enum
	}{
		{"string", enum(g.String, g.EnumValues, g.EnumMember{Name: "Greeting", Value: constant.MakeString("hello\"雪")})},
		{"signed_boundary", enum(g.Int64, g.EnumValues, g.EnumMember{Name: "Min", Value: integer("-9223372036854775808")})},
		{"unsigned_boundary", enum(g.Uint64, g.EnumValues, g.EnumMember{Name: "Max", Value: integer("18446744073709551615")})},
		{"outside_javascript_precision", enum(g.Int64, g.EnumValues, g.EnumMember{Name: "Exact", Value: integer("9007199254740993")})},
		{"numeric_duplicates", enum(g.Int, g.EnumValues, g.EnumMember{Name: "One", Value: integer("1")}, g.EnumMember{Name: "AlsoOne", Value: integer("1")})},
		{"string_mode", enum(g.Int8, g.EnumNames, g.EnumMember{Name: "Ready", Value: integer("0")}, g.EnumMember{Name: "Done", Value: integer("1")})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireValid(t, g.Definitions{definition("State", tc.typ)})
		})
	}
}

func TestValidateRejectsInvalidEnums(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  *g.Enum
	}{
		{"empty", enum(g.Int, g.EnumValues)},
		{"invalid_mode", enum(g.Int, g.EnumMode(99), g.EnumMember{Name: "One", Value: integer("1")})},
		{"unsupported_kind", enum(g.Float64, g.EnumValues, g.EnumMember{Name: "One", Value: integer("1")})},
		{"nil_constant", enum(g.Int, g.EnumValues, g.EnumMember{Name: "One"})},
		{"unknown_constant", enum(g.Int, g.EnumValues, g.EnumMember{Name: "One", Value: constant.MakeUnknown()})},
		{"wrong_integer_kind", enum(g.Int, g.EnumValues, g.EnumMember{Name: "One", Value: constant.MakeString("1")})},
		{"wrong_string_kind", enum(g.String, g.EnumValues, g.EnumMember{Name: "One", Value: integer("1")})},
		{"float_constant", enum(g.Int, g.EnumValues, g.EnumMember{Name: "Half", Value: constant.MakeFloat64(0.5)})},
		{"int8_overflow", enum(g.Int8, g.EnumValues, g.EnumMember{Name: "TooBig", Value: integer("128")})},
		{"int64_overflow", enum(g.Int64, g.EnumValues, g.EnumMember{Name: "TooBig", Value: integer("9223372036854775808")})},
		{"negative_unsigned", enum(g.Uint, g.EnumValues, g.EnumMember{Name: "Negative", Value: integer("-1")})},
		{"uint64_overflow", enum(g.Uint64, g.EnumValues, g.EnumMember{Name: "TooBig", Value: integer("18446744073709551616")})},
		{"duplicate_names", enum(g.Int, g.EnumValues, g.EnumMember{Name: "Same", Value: integer("1")}, g.EnumMember{Name: "Same", Value: integer("2")})},
		{"ambiguous_names_mode", enum(g.Int, g.EnumNames, g.EnumMember{Name: "One", Value: integer("1")}, g.EnumMember{Name: "AlsoOne", Value: integer("1")})},
		{"names_mode_string", enum(g.String, g.EnumNames, g.EnumMember{Name: "One", Value: constant.MakeString("one")})},
	} {
		t.Run(tc.name, func(t *testing.T) { _ = requireInvalid(t, g.Definitions{definition("State", tc.typ)}) })
	}
}

func TestValidateEnumContext(t *testing.T) {
	registered := enum(g.Int, g.EnumNames, g.EnumMember{Name: "Ready", Value: integer("0")})
	for _, tc := range []struct {
		name  string
		value g.FieldValue
	}{
		{"required", &g.Required{Type: registered}},
		{"optional", &g.Optional{Type: registered}},
		{"nullable", &g.Nullable{Type: registered}},
		{"reference", &g.Required{Type: &g.Ref{Target: name("State")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireValid(t, g.Definitions{
				definition("State", registered),
				definition("Owner", &g.Object{Fields: []g.Field{{GoName: "Value", JSONName: "value", Value: tc.value}}}),
			})
		})
	}
	for _, wrap := range []struct {
		name string
		wrap func(g.Type) g.Type
	}{
		{"pointer", func(t g.Type) g.Type { return &g.Pointer{Element: t} }},
		{"array", func(t g.Type) g.Type { return &g.Array{Length: 2, Element: t} }},
		{"slice", func(t g.Type) g.Type { return &g.Slice{Element: t} }},
	} {
		for _, reference := range []bool{false, true} {
			t.Run(wrap.name+map[bool]string{false: "/inline", true: "/reference"}[reference], func(t *testing.T) {
				var value g.Type = registered
				if reference {
					value = &g.Ref{Target: name("State")}
				}
				_ = requireInvalid(t, g.Definitions{definition("State", registered), definition("Values", wrap.wrap(value))})
				// A named underlying scalar carries no enum registration to adapt.
				requireValid(t, g.Definitions{
					definition("Plain", &g.Scalar{Kind: g.Int}),
					definition("Values", wrap.wrap(&g.Ref{Target: name("Plain")})),
				})
			})
		}
		t.Run(wrap.name+"/numeric_enum", func(t *testing.T) {
			numeric := enum(g.Int, g.EnumValues, g.EnumMember{Name: "Ready", Value: integer("0")})
			requireValid(t, g.Definitions{definition("State", numeric), definition("Values", wrap.wrap(&g.Ref{Target: name("State")}))})
		})
	}
}

func TestValidateEnumOwnerContext(t *testing.T) {
	for _, form := range []string{"required", "optional", "nullable"} {
		t.Run("shared_owner_becomes_inline/"+form, func(t *testing.T) {
			typ := enum(g.Int, g.EnumNames, g.EnumMember{Name: "Ready", Value: integer("0")})
			var value g.FieldValue
			switch form {
			case "required":
				value = &g.Required{Type: typ}
			case "optional":
				value = &g.Optional{Type: typ}
			case "nullable":
				value = &g.Nullable{Type: typ}
			}
			shared := &g.Object{Fields: []g.Field{{GoName: "State", JSONName: "state", Value: value}}}
			// The first definition is valid. Reusing its exact node without a
			// named reference must still check the anonymous-owner restriction.
			requireValid(t, g.Definitions{definition("Owner", shared)})
			_ = requireInvalid(t, g.Definitions{
				definition("Owner", shared),
				definition("Envelope", &g.Object{Fields: []g.Field{required("Inline", "inline", shared)}}),
			})
		})
	}
	for _, tc := range []struct {
		name  string
		mode  g.EnumMode
		valid bool
	}{
		{"anonymous_names_adapter", g.EnumNames, false},
		{"anonymous_numeric_values", g.EnumValues, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inline := &g.Object{Fields: []g.Field{required("State", "state", enum(g.Int, tc.mode, g.EnumMember{Name: "Ready", Value: integer("0")}))}}
			defs := g.Definitions{definition("Envelope", &g.Object{Fields: []g.Field{required("Inline", "inline", inline)}})}
			if tc.valid {
				requireValid(t, defs)
			} else {
				_ = requireInvalid(t, defs)
			}
		})
	}
	t.Run("named_owner_nested_in_collection", func(t *testing.T) {
		owner := &g.Object{Fields: []g.Field{required("State", "state", enum(g.Int, g.EnumNames, g.EnumMember{Name: "Ready", Value: integer("0")}))}}
		requireValid(t, g.Definitions{
			definition("Owner", owner),
			definition("Envelope", &g.Object{Fields: []g.Field{required("Owners", "owners", &g.Slice{Element: &g.Ref{Target: name("Owner")}})}}),
		})
	})
}

func TestValidateDiagnosticsPreserveSource(t *testing.T) {
	defSource := token.Position{Filename: "model.go", Line: 10, Column: 1}
	fieldSource := token.Position{Filename: "model.go", Line: 14, Column: 3}
	owner := definition("Owner", &g.Object{Fields: []g.Field{{
		GoName: "Value", JSONName: "value", Source: fieldSource,
		Value: &g.Required{Type: &g.Ref{Target: name("Missing")}},
	}}})
	owner.Source = defSource
	diagnostic := requireInvalid(t, g.Definitions{owner})
	if diagnostic.Source != fieldSource {
		t.Fatalf("source = %v, want field source %v", diagnostic.Source, fieldSource)
	}
	if !strings.Contains(diagnostic.Path, "Owner") || !strings.Contains(diagnostic.Path, "Value") {
		t.Fatalf("path does not identify the owner and field: %q", diagnostic.Path)
	}
	owner.Type.(*g.Object).Fields[0].Source = token.Position{}
	diagnostic = requireInvalid(t, g.Definitions{owner})
	if diagnostic.Source != defSource {
		t.Fatalf("source = %v, want enclosing definition source %v", diagnostic.Source, defSource)
	}
	if !strings.Contains(diagnostic.Error(), "model.go") {
		t.Fatalf("formatted error drops the source location: %v", diagnostic)
	}
}
