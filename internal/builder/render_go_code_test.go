package builder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/typegrammar"
)

// These tests render generated Go code from schemaTemplateData values built
// by hand. Nothing is loaded or type-checked: renderGoCode is a pure function
// of its argument, so the shape of the generated file is proven here in
// microseconds, and the load-based tests only have to prove that the builder
// resolves the right data (issue #132).

const renderFixturePkg = "example.com/fixture"

func renderName(name string) typegrammar.Name {
	return typegrammar.Name{PackagePath: renderFixturePkg, Name: name}
}

// shapeUnion is a sealed Shape interface with a value variant and a pointer
// variant, discriminated by property.
func shapeUnion(property string) typegrammar.Union {
	return typegrammar.Union{
		Interface:     renderName("Shape"),
		Discriminator: property,
		Variants: []typegrammar.Variant{
			{Implementation: renderName("Circle"), Tag: "Circle"},
			{Implementation: renderName("Square"), Tag: "Square", Pointer: true},
		},
	}
}

// unionField is one union field of an owner codec as the codec planner
// records it: Go and JSON names, the struct tag, the embedded path it was
// promoted through, and the discriminator the interface declared ("" for the
// package default).
func unionField(goName, jsonName, tag string, path []string, union typegrammar.Union, declared string) InterfaceProp {
	return InterfaceProp{
		goName:                      goName,
		jsonName:                    jsonName,
		tag:                         tag,
		path:                        path,
		Union:                       union,
		DiscPropName:                declared,
		interfacePkgName:            "fixture",
		InterfaceTypeNameWithPrefix: union.Interface.Name,
	}
}

// helperFor is the generated marshal/unmarshal helper pair a union field
// resolves to, as templateData derives it from the field.
func helperFor(prop InterfaceProp) InterfaceInfo {
	options := make([]InterfaceOptionInfo, 0, len(prop.Union.Variants))
	for _, variant := range prop.Union.Variants {
		options = append(options, InterfaceOptionInfo{
			TypeNameWithPrefix: variant.Implementation.Name,
			Discriminator:      variant.Tag,
			Pointer:            variant.Pointer,
		})
	}
	return InterfaceInfo{
		TypeNameWithPrefix:    prop.InterfaceTypeNameWithPrefix,
		TypeName:              prop.Union.Interface.Name,
		MarshalerFunc:         prop.MarshalerFunc(),
		UnmarshalerFunc:       prop.UnmarshalerFunc(),
		DiscriminatorPropName: prop.DiscPropName,
		Options:               options,
	}
}

func nameModeEnumField(goName, jsonName, tag string) EnumFieldPlan {
	return EnumFieldPlan{
		goName:                 goName,
		jsonName:               jsonName,
		tag:                    tag,
		EnumType:               renderName("Level"),
		EnumTypeName:           "Level",
		EnumTypeNameWithPrefix: "Level",
		Entries: []EnumEntry{
			{ConstName: "LevelLow", GoValueExpr: "LevelLow", WireName: "LevelLow"},
			{ConstName: "LevelHigh", GoValueExpr: "LevelHigh", WireName: "LevelHigh"},
		},
		MarshalerFunc:   "__jsonMarshalEnum__Root__" + goName,
		UnmarshalerFunc: "__jsonUnmarshalEnum__Root__" + goName,
	}
}

// codecOnlyData is the generated file of a package whose roots have no
// schema entrypoint: codecs only, no embedded assets.
func codecOnlyData() schemaTemplateData {
	return schemaTemplateData{
		PackageName:       "fixture",
		BuildTag:          "jsonschema",
		Subdir:            "jsonschema",
		DiscriminatorProp: "type",
	}
}

// rootCodecData is one owner codec covering every union and enum field form
// the template renders, promoted fields included.
func rootCodecData() schemaTemplateData {
	union := shapeUnion("type")
	body := unionField("Body", "body", "`json:\"body\"`", nil, union, "")
	header := unionField("Header", "header", "`json:\"header\"`", []string{"Base"}, union, "")
	footer := unionField("Footer", "footer", "`json:\"footer,omitzero\"`", nil, union, "")
	footer.Optional = true
	shapes := unionField("Shapes", "shapes", "`json:\"shapes\"`", nil, union, "")
	shapes.Repeated = true

	either := nameModeEnumField("Either", "either", "`json:\"either\"`")
	either.nullable = true
	level := nameModeEnumField("Level", "level", "`json:\"level\"`")
	maybe := nameModeEnumField("Maybe", "maybe", "`json:\"maybe,omitzero\"`")
	maybe.optional = true

	data := codecOnlyData()
	data.OwnerCodecs = []OwnerCodec{{
		Name:        "Root",
		Initial:     "r",
		UnionFields: []InterfaceProp{body, header, footer, shapes},
		EnumFields:  []EnumFieldPlan{either, level, maybe},
	}}
	data.Interfaces = []InterfaceInfo{helperFor(body)}
	return data
}

func render(t *testing.T, data schemaTemplateData) string {
	t.Helper()
	code, err := renderGoCode(data)
	require.NoError(t, err)
	return string(code)
}

func requireAll(t *testing.T, code string, present []string, absent []string) {
	t.Helper()
	for _, want := range present {
		require.Contains(t, code, want, "generated code:\n%s", code)
	}
	for _, unwanted := range absent {
		require.NotContains(t, code, unwanted, "generated code:\n%s", code)
	}
}

// TestRenderGoCodeCodecOnlyFile proves a package without a schema entrypoint
// gets codecs and their helpers, and none of the schema assets: no embed.FS,
// no accessors, no validation.
func TestRenderGoCodeCodecOnlyFile(t *testing.T) {
	t.Parallel()
	data := rootCodecData()
	code := render(t, data)
	body := data.OwnerCodecs[0].UnionFields[0]

	require.True(t, strings.HasPrefix(code, "//go:build !jsonschema\n\n// Code generated by polytype. DO NOT EDIT.\n\npackage fixture\n"), code)
	requireAll(t, code, []string{
		`jsonv2 "encoding/json/v2"`,
		"func __polytype_marshal(value any) ([]byte, error) {",
		"func (r Root) MarshalJSON() ([]byte, error) {",
		"func (r *Root) UnmarshalJSON(data []byte) (err error) {",
		"func " + body.MarshalerFunc() + "(value Shape) (json.RawMessage, error) {",
		"func " + body.UnmarshalerFunc() + "(data []byte) (Shape, error) {",
		"func __jsonschema__marshalUnionObject(data []byte, discriminatorProp, discriminatorValue string) (json.RawMessage, error) {",
		"func __jsonschema__isJSONNull(data []byte) bool {",
	}, []string{
		"go:embed", `"embed"`, "__gen_jsonschema_fs", "ValidateJSON", "RenderedSchema", "jsonschema.Schema", "errNoDiscriminator",
	})
}

// TestRenderGoCodeOwnerCodecCoversEveryFieldForm pins how each union and
// enum field form is adapted: the wrapper struct, the accessor through the
// embedded path, and the optional, nullable and repeated branches.
func TestRenderGoCodeOwnerCodecCoversEveryFieldForm(t *testing.T) {
	t.Parallel()
	data := rootCodecData()
	code := render(t, data)
	marshal := data.OwnerCodecs[0].UnionFields[0].MarshalerFunc()
	unmarshal := data.OwnerCodecs[0].UnionFields[0].UnmarshalerFunc()

	requireAll(t, code, []string{
		// The wrapper redeclares every adapted field as raw JSON, tag intact.
		"\t\tHeader json.RawMessage `json:\"header\"`\n",
		"\t\tFooter json.RawMessage `json:\"footer,omitzero\"`\n",
		"\t\tMaybe  json.RawMessage `json:\"maybe,omitzero\"`\n",
		// A required union field, and one promoted through Base.
		"if wrapper.Body, err = " + marshal + "(r.Body); err != nil {",
		"if wrapper.Header, err = " + marshal + "(r.Base.Header); err != nil {",
		"__next.Base.Header = __decoded1",
		// Optional[I] round-trips through Present/Value.
		"if r.Footer.Present {",
		"(r.Footer.Value); err != nil {",
		"__next.Footer.Value = __decoded2",
		"__next.Footer.Present = true",
		// []I is encoded element by element and rejects a nil slice.
		"if r.Shapes == nil {",
		`return nil, fmt.Errorf("field shapes: nil registered interface slice")`,
		"__raw3 := make([]json.RawMessage, len(r.Shapes))",
		"if __decoded3[__index], err = " + unmarshal + "(__raw); err != nil {",
		// Name-mode enum fields: required, Optional[E] and Nullable[E].
		"func __jsonMarshalEnum__Root__Level(value Level) (json.RawMessage, error) {",
		"func __jsonUnmarshalEnum__Root__Level(data []byte) (Level, error) {",
		"case LevelHigh:\n\t\treturn __polytype_marshal(\"LevelHigh\")",
		"case \"LevelHigh\":\n\t\treturn LevelHigh, nil",
		`return zero, errors.New("string-mode enum Level cannot be JSON null")`,
		"__next.Level = __enumDecoded1",
		"if r.Maybe.Present {",
		`return fmt.Errorf("field maybe: Optional value cannot be JSON null")`,
		`wrapper.Either = json.RawMessage("null")`,
		`return fmt.Errorf("field either: required nullable string-mode enum is missing")`,
	}, nil)
}

// TestRenderGoCodeUnionHelpersConstructEachVariantByReceiver proves the
// helper switches on the concrete type for a value variant and on the
// pointer for a pointer variant, and decodes each accordingly.
func TestRenderGoCodeUnionHelpersConstructEachVariantByReceiver(t *testing.T) {
	t.Parallel()
	code := render(t, rootCodecData())
	requireAll(t, code, []string{
		"case Circle:\n\t\tdiscriminator = \"Circle\"\n\t\tdata, err = __polytype_marshal(&object)",
		"case *Square:\n\t\tif object == nil {",
		"discriminator = \"Square\"\n\t\tdata, err = __polytype_marshal(object)",
		"case \"Circle\":\n\t\tvar obj Circle\n\t\tif err = json.Unmarshal(data, &obj); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\treturn obj, nil",
		"case \"Square\":\n\t\tvar obj Square\n\t\tif err = json.Unmarshal(data, &obj); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\treturn &obj, nil",
		`return nil, fmt.Errorf("unregistered dynamic implementation %T for Shape", value)`,
	}, nil)
}

// TestRenderGoCodeUnionHelpersUseTheDeclaredDiscriminator proves a helper
// reads the property its interface declared, and otherwise the package
// default, never the union's own field.
func TestRenderGoCodeUnionHelpersUseTheDeclaredDiscriminator(t *testing.T) {
	t.Parallel()
	declared := unionField("Value", "value", "`json:\"value\"`", nil, shapeUnion("kind"), "kind")
	data := codecOnlyData()
	data.DiscriminatorProp = "variant"
	data.OwnerCodecs = []OwnerCodec{{Name: "Owner", Initial: "o", UnionFields: []InterfaceProp{declared}}}
	data.Interfaces = []InterfaceInfo{helperFor(declared)}
	requireAll(t, render(t, data), []string{
		`} else if _tempDiscriminator, ok := temp["kind"]; !ok {`,
		`return nil, fmt.Errorf("no discriminator property '%s' found", "kind")`,
		"return __jsonschema__marshalUnionObject(data,\n\t\t\"kind\",\n\t\tdiscriminator,\n\t)",
	}, []string{`temp["variant"]`, `temp["type"]`})

	defaulted := unionField("Value", "value", "`json:\"value\"`", nil, shapeUnion("variant"), "")
	data.OwnerCodecs = []OwnerCodec{{Name: "Owner", Initial: "o", UnionFields: []InterfaceProp{defaulted}}}
	data.Interfaces = []InterfaceInfo{helperFor(defaulted)}
	requireAll(t, render(t, data), []string{
		`} else if _tempDiscriminator, ok := temp["variant"]; !ok {`,
		"return __jsonschema__marshalUnionObject(data,\n\t\t\"variant\",\n\t\tdiscriminator,\n\t)",
	}, []string{`temp["kind"]`})
}

// TestRenderGoCodeSchemaAccessorsValidationAndRenderedSchemas proves the
// schema half of the file: the embedded FS, one accessor per entrypoint as a
// value method, pointer method, template accessor or free function, compiled
// schemas and ValidateJSON for every non-rendered root only, and
// RenderedSchema calling each provider kind.
func TestRenderGoCodeSchemaAccessorsValidationAndRenderedSchemas(t *testing.T) {
	t.Parallel()
	data := codecOnlyData()
	data.GenerateSchemas = true
	data.Validate = true
	data.SchemaMethods = []SchemaAccessor{
		{TypeName: "Person", MethodName: "Schema"},
		{TypeName: "Account", MethodName: "AccountSchema", Pointer: true},
		{TypeName: "Example", MethodName: "Schema"},
	}
	data.SchemaFreeFuncs = []SchemaAccessor{{TypeName: "PointerRoot", MethodName: "PointerRootSchema"}}
	data.Rendered = map[string]bool{"Example": true}
	data.RenderedTypes = []string{"Example"}
	data.TypeProviders = []TypeProviders{{TypeName: "Example", Providers: []FieldProvider{
		{FieldName: "A", JSONName: "a", Kind: "WithStructAccessorMethod", ProviderName: "ASchema", ProviderIsMethod: true},
		{FieldName: "B", JSONName: "b", Kind: "WithStructFunctionMethod", ProviderName: "BSchema", ProviderIsMethod: true},
		{FieldName: "C", JSONName: "c", Kind: "WithFunction", ProviderName: "BoolSchemaFunc"},
	}}}

	requireAll(t, render(t, data), []string{
		"//go:embed jsonschema\nvar __gen_jsonschema_fs embed.FS",
		`jsonschema "github.com/santhosh-tekuri/jsonschema/v6"`,
		"func (Person) Schema() json.RawMessage {",
		`const fileName = "jsonschema/Person.json"`,
		"func (*Account) AccountSchema() json.RawMessage {",
		"func (Example) Schema() json.RawMessage {",
		`const fileName = "jsonschema/Example.json.tmpl"`,
		"func PointerRootSchema(PointerRoot) json.RawMessage {",
		"\t__gen_jsonschema_compiled_Person  *jsonschema.Schema\n\t__gen_jsonschema_compiled_Account *jsonschema.Schema\n",
		`__gen_jsonschema_compiled_Person = compile("Person", __zero.Schema())`,
		`__gen_jsonschema_compiled_Account = compile("Account", __zero.AccountSchema())`,
		"func (Person) ValidateJSON(data []byte) error {",
		"func (Account) ValidateJSON(data []byte) error {",
		"func (t Example) RenderedSchema() (json.RawMessage, error) {",
		"__prov = t.ASchema()",
		"__prov = t.BSchema(t.B)",
		"__prov = BoolSchemaFunc(t.C)",
		`ctx["c"] = string(__b)`,
		`tpl, err := template.New("schema").Option("missingkey=error").Parse(string(data))`,
	}, []string{
		"__gen_jsonschema_compiled_Example",
		"func (Example) ValidateJSON",
		"func (PointerRoot) ValidateJSON",
		"func (PointerRoot) PointerRootSchema",
		"func (t Person) RenderedSchema",
		") MarshalJSON() ([]byte, error) {",
		"__polytype_marshal",
	})
}

// TestRenderGoCodeValidationIsSkippedWhenEveryRootIsRendered proves a
// package whose only roots are provider templates compiles no schemas: a
// rendered schema depends on runtime values and cannot be validated ahead.
func TestRenderGoCodeValidationIsSkippedWhenEveryRootIsRendered(t *testing.T) {
	t.Parallel()
	data := codecOnlyData()
	data.GenerateSchemas = true
	data.Validate = true
	data.SchemaMethods = []SchemaAccessor{{TypeName: "Example", MethodName: "Schema"}}
	data.Rendered = map[string]bool{"Example": true}
	data.RenderedTypes = []string{"Example"}
	data.TypeProviders = []TypeProviders{{TypeName: "Example", Providers: []FieldProvider{
		{FieldName: "A", JSONName: "a", Kind: "WithStructAccessorMethod", ProviderName: "ASchema", ProviderIsMethod: true},
	}}}
	require.False(t, data.HasNonRenderedTypes())
	requireAll(t, render(t, data), []string{
		"func (t Example) RenderedSchema() (json.RawMessage, error) {",
	}, []string{"jsonschema.Schema", "ValidateJSON", "func init()"})
}

// TestRenderGoCodeEnumMarkers pins the type-level enum codecs: every marked
// type is referenced through its first constant (never *new(T)), a string
// enum and an integer enum encode their constants' own values and reject
// anything else by name, and a marker without encodable members gets the
// assertion but no codec.
func TestRenderGoCodeEnumMarkers(t *testing.T) {
	t.Parallel()
	data := codecOnlyData()
	data.EnumMarkers = []EnumMarker{
		{TypeName: "Color", Constant: "ColorRed", Underlying: "int", Members: []EnumMember{
			{Constant: "ColorRed", Wire: "1"}, {Constant: "ColorBlue", Wire: "4"},
		}},
		{TypeName: "Size", Constant: "SizeSmall", Underlying: "string", IsString: true, Members: []EnumMember{
			{Constant: "SizeSmall", Wire: `"small"`}, {Constant: "SizeLarge", Wire: `"large"`},
		}},
		{TypeName: "unencodable", Constant: "unencodableFirst"},
	}
	requireAll(t, render(t, data), []string{
		"\t_ interface{ enum() } = ColorRed\n",
		"\t_ interface{ enum() } = SizeSmall\n",
		"\t_ interface{ enum() } = unencodableFirst\n",
		"func (__enumValue Color) MarshalJSON() ([]byte, error) {",
		"func (__enumValue *Color) UnmarshalJSON(data []byte) error {",
		"func (__enumValue Size) MarshalJSON() ([]byte, error) {",
		"func (__enumValue *Size) UnmarshalJSON(data []byte) error {",
		"case ColorBlue:\n\t\treturn []byte(\"4\"), nil",
		"case SizeLarge:\n\t\treturn []byte(\"\\\"large\\\"\"), nil",
		`is not a declared member of enum Color", int(__enumValue))`,
		`is not a declared member of enum Size", string(__enumValue))`,
		"var __wire *int\n",
		"var __wire *string\n",
		`return errors.New("polytype: enum Color cannot be JSON null")`,
	}, []string{
		"*new(",
		"func (__enumValue unencodable) MarshalJSON",
		"__polytype_marshal",
		`"encoding/json/v2"`,
	})
}

// TestRenderGoCodeIsDeterministic proves rendering is a function of the data
// alone: two renders of one value are byte-identical and neither leaves
// anything behind in the value, so codecs and helpers are never duplicated
// by rendering twice (the regression behind gen_schema_determinism_test).
func TestRenderGoCodeIsDeterministic(t *testing.T) {
	t.Parallel()
	data := rootCodecData()
	first := render(t, data)
	second := render(t, data)
	require.Equal(t, first, second)
	require.Len(t, data.OwnerCodecs, 1)
	require.Len(t, data.Interfaces, 1)
	require.Equal(t, 1, strings.Count(second, "func (r Root) MarshalJSON()"))
	require.Equal(t, 1, strings.Count(second, "func (r *Root) UnmarshalJSON(data []byte)"))
	require.Equal(t, 1, strings.Count(second, "func "+data.Interfaces[0].UnmarshalerFunc+"("))
}

// TestUnionHelperNamesFollowResolvedInterfaceIdentity proves the generated
// helper is keyed on the registration a field resolves to, not on the field:
// two fields of one interface share a helper, while a same-named interface
// from another package, a declared discriminator, or a different variant
// receiver each get their own.
func TestUnionHelperNamesFollowResolvedInterfaceIdentity(t *testing.T) {
	t.Parallel()
	eventUnion := func(pkgPath string) typegrammar.Union {
		return typegrammar.Union{
			Interface:     typegrammar.Name{PackagePath: pkgPath, Name: "Event"},
			Discriminator: "type",
			Variants: []typegrammar.Variant{
				{Implementation: typegrammar.Name{PackagePath: pkgPath, Name: "Created"}, Tag: "Created"},
			},
		}
	}
	left := unionField("Left", "left", "`json:\"left\"`", nil, eventUnion("example.com/left"), "")
	left.interfacePkgName = "events"
	again := unionField("Again", "again", "`json:\"again\"`", []string{"Base"}, eventUnion("example.com/left"), "")
	again.interfacePkgName = "events"
	right := unionField("Right", "right", "`json:\"right\"`", nil, eventUnion("example.com/right"), "")
	right.interfacePkgName = "events"
	declared := left
	declared.DiscPropName = "kind"
	pointer := left
	pointer.Union.Variants = []typegrammar.Variant{{Implementation: pointer.Union.Variants[0].Implementation, Tag: "Created", Pointer: true}}

	require.True(t, strings.HasPrefix(left.UnmarshalerFunc(), "__jsonUnmarshal__events__Event__"), left.UnmarshalerFunc())
	require.Equal(t, strings.Replace(left.UnmarshalerFunc(), "__jsonUnmarshal__", "__jsonMarshal__", 1), left.MarshalerFunc())
	require.Equal(t, left.UnmarshalerFunc(), again.UnmarshalerFunc(), "one registration, one helper")
	require.NotEqual(t, left.UnmarshalerFunc(), right.UnmarshalerFunc(), "same-named interfaces in different packages")
	require.NotEqual(t, left.UnmarshalerFunc(), declared.UnmarshalerFunc(), "a declared discriminator changes the helper")
	require.NotEqual(t, left.UnmarshalerFunc(), pointer.UnmarshalerFunc(), "a pointer variant changes the helper")
}
