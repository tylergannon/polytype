package builder

import (
	"encoding/json"
	"fmt"
	"go/constant"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

func TestTypeDefinitionsPreservesRegisteredSourceGrammar(t *testing.T) {
	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"
	"time"

	"github.com/tylergannon/polytype"
)

type State int16

const (
	StateLow State = iota
	StateHigh
	StateUnrelated = 3
	StateUrgent = State(1 << 3)
	StateMedium State = 4
)

func (State) enum() {}

// Label is the globally registered label enum.
type Label string

const (
	// LabelFirst is the first label.
	LabelFirst Label = "first"
	LabelUnrelated = "not-a-label"
)

// LabelLast is conversion-typed.
const LabelLast = Label("la" + "st")

type LabelAlias = Label

func (Label) enum() {}

type Huge uint64

const HugeMax Huge = 18446744073709551615

func (Huge) enum() {}

type Event interface{ event() }

type Created struct {
	Kind string ` + "`json:\"label\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

func (*Created) event() {}

type Deleted struct {
	At polytype.Nullable[time.Time] ` + "`json:\"at\"`" + `
}

func (Deleted) event() {}

type Meta struct {
	Code   uint32 ` + "`json:\"code\"`" + `
	Shadow string ` + "`json:\"shadow\"`" + `
}

type EmbedA struct {
	Clash  string
	Choice string ` + "`json:\"Choice\"`" + `
}

type EmbedB struct {
	Clash  int
	Choice int
}

type Envelope struct {
	Meta
	EmbedA
	EmbedB
	Event  Event                       ` + "`json:\"event\"`" + `
	Maybe  polytype.Optional[Event]  ` + "`json:\"maybe,omitzero\"`" + `
	Events []Event                     ` + "`json:\"events\"`" + `
	// State has a field comment that prior field-local schema output omitted.
	State  State                       ` + "`json:\"state\"`" + `
	// Raw also keeps the prior description-free field-local schema shape.
	Raw    State                       ` + "`json:\"raw\"`" + `
	Labels []Label                     ` + "`json:\"labels\"`" + `
	Alias  LabelAlias                  ` + "`json:\"alias\"`" + `
	Huge   Huge                        ` + "`json:\"huge\"`" + `
	Count  polytype.Nullable[int64]  ` + "`json:\"count\"`" + `
	Child  polytype.Nullable[*Meta]    ` + "`json:\"child\"`" + `
	Matrix [2][]int16                  ` + "`json:\"matrix\"`" + `
	Shadow int                         ` + "`json:\"shadow\"`" + `
	hidden, Exported int
}

func (Envelope) Schema() json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewJSONSchemaMethod(
		Envelope.Schema,
		polytype.WithStringerEnum(Envelope{}.State),
	)
	_ = polytype.SealedUnion[Event]("kind")
)
`

	builder := loadTypeGrammarFixture(t, source)
	defs, err := builder.TypeDefinitions()
	require.NoError(t, err)
	require.NoError(t, defs.Validate())

	envelope := requireDefinition(t, defs, "Envelope")
	object, ok := envelope.Type.(*typegrammar.Object)
	require.True(t, ok)
	require.Equal(t, []string{"code", "Choice", "event", "maybe", "events", "state", "raw", "labels", "alias", "huge", "count", "child", "matrix", "shadow", "Exported"}, fieldJSONNames(object.Fields))
	require.Equal(t, "Exported", requireField(t, object.Fields, "Exported").GoName)
	require.Equal(t, typegrammar.String, fieldType[*typegrammar.Scalar](t, requireField(t, object.Fields, "Choice").Value).Kind)

	event := requireField(t, object.Fields, "event")
	direct, ok := event.Value.(*typegrammar.Union)
	require.True(t, ok)
	require.Equal(t, "kind", direct.Discriminator)
	require.Len(t, direct.Variants, 2)
	require.Equal(t, "Created", direct.Variants[0].Tag)
	require.True(t, direct.Variants[0].Pointer)
	require.Equal(t, "Deleted", direct.Variants[1].Tag)
	require.False(t, direct.Variants[1].Pointer)

	_, ok = requireField(t, object.Fields, "maybe").Value.(*typegrammar.OptionalUnion)
	require.True(t, ok)
	_, ok = requireField(t, object.Fields, "events").Value.(*typegrammar.UnionSlice)
	require.True(t, ok)

	state := fieldType[*typegrammar.Enum](t, requireField(t, object.Fields, "state").Value)
	require.Equal(t, typegrammar.EnumNames, state.Mode)
	require.Equal(t, typegrammar.Int16, state.Kind)
	require.Equal(t, []string{"StateLow", "StateHigh", "StateUrgent", "StateMedium"}, enumMemberNames(state.Members))
	require.Equal(t, []string{"0", "1", "8", "4"}, enumMemberValues(state.Members))

	// Fields of a marked enum type with no field-level declaration refer to
	// the type's own definition, which is an enum in value mode.
	rawRef := fieldType[*typegrammar.Ref](t, requireField(t, object.Fields, "raw").Value)
	require.Equal(t, "State", rawRef.Target.Name)
	raw, ok := requireDefinition(t, defs, "State").Type.(*typegrammar.Enum)
	require.True(t, ok)
	require.Equal(t, typegrammar.EnumValues, raw.Mode)
	require.Equal(t, []string{"0", "1", "8", "4"}, enumMemberValues(raw.Members))
	hugeRef := fieldType[*typegrammar.Ref](t, requireField(t, object.Fields, "huge").Value)
	require.Equal(t, "Huge", hugeRef.Target.Name)
	huge, ok := requireDefinition(t, defs, "Huge").Type.(*typegrammar.Enum)
	require.True(t, ok)
	require.Equal(t, []string{"18446744073709551615"}, enumMemberValues(huge.Members))

	labels := fieldType[*typegrammar.Slice](t, requireField(t, object.Fields, "labels").Value)
	labelRef, ok := labels.Element.(*typegrammar.Ref)
	require.True(t, ok)
	require.Equal(t, "Label", labelRef.Target.Name)
	labelDef := requireDefinition(t, defs, "Label")
	labelEnum, ok := labelDef.Type.(*typegrammar.Enum)
	require.True(t, ok)
	require.Equal(t, constant.String, labelEnum.Members[0].Value.Kind())
	require.Equal(t, "first", constant.StringVal(labelEnum.Members[0].Value))
	require.Equal(t, "last", constant.StringVal(labelEnum.Members[1].Value))
	require.Equal(t, []string{"LabelFirst", "LabelLast"}, enumMemberNames(labelEnum.Members))
	aliasDef := requireDefinition(t, defs, "LabelAlias")
	aliasEnum, ok := aliasDef.Type.(*typegrammar.Enum)
	require.True(t, ok)
	require.Equal(t, []string{"LabelFirst", "LabelLast"}, enumMemberNames(aliasEnum.Members))

	schema, ok := builder.GetSchema(syntax.TypeID{PkgPath: builder.Scan.Pkg.PkgPath, TypeName: "Envelope"})
	require.True(t, ok)
	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	require.Contains(t, string(schemaJSON), `"huge":{"type":"integer","enum":[18446744073709551615]}`)
	var rendered struct {
		Properties map[string]struct {
			Enum        []any  `json:"enum"`
			Description string `json:"description"`
			Items       struct {
				Enum        []any  `json:"enum"`
				Description string `json:"description"`
			} `json:"items"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(schemaJSON, &rendered))
	require.Equal(t, []any{"StateLow", "StateHigh", "StateUrgent", "StateMedium"}, rendered.Properties["state"].Enum)
	require.Equal(t, []any{float64(0), float64(1), float64(8), float64(4)}, rendered.Properties["raw"].Enum)
	require.Empty(t, rendered.Properties["state"].Description)
	require.Contains(t, rendered.Properties["raw"].Description, "Raw also keeps")
	require.Equal(t, []any{"first", "last"}, rendered.Properties["labels"].Items.Enum)
	require.Contains(t, rendered.Properties["labels"].Items.Description, "Label is the globally registered label enum")
	require.Contains(t, rendered.Properties["labels"].Items.Description, "LabelFirst is the first label")
	require.Equal(t, []any{"first", "last"}, rendered.Properties["alias"].Enum)

	_, ok = requireField(t, object.Fields, "count").Value.(*typegrammar.Nullable)
	require.True(t, ok)
	matrix := fieldType[*typegrammar.Array](t, requireField(t, object.Fields, "matrix").Value)
	require.Equal(t, int64(2), matrix.Length)
	_, ok = matrix.Element.(*typegrammar.Slice)
	require.True(t, ok)

	for _, name := range []string{"Envelope", "Meta", "EmbedA", "EmbedB", "Created", "Deleted", "State", "Label", "LabelAlias", "Huge"} {
		requireDefinition(t, defs, name)
	}
}

func TestTypeDefinitionsRejectsUnresolvedWireMappings(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "byte slice",
			body: `type Root struct { Data []uint8 ` + "`json:\"data\"`" + ` }`,
			want: "byte-like slices have a base64 wire mapping outside this grammar",
		},
		{
			name: "json string coercion",
			body: `type Root struct { Count int ` + "`json:\"count,string\"`" + ` }`,
			want: `uses json:",string"`,
		},
		{
			name: "explicit ref",
			body: `
type External struct { Value string ` + "`json:\"value\"`" + ` }
type Root struct { Value External ` + "`json:\"value\" jsonschema:\"ref=https://example.test/schema\"`" + ` }
`,
			want: "explicit schema ref with no resolved static type target",
		},
		{
			name: "custom codec",
			body: `
type Custom int
func (Custom) MarshalJSON() ([]byte, error) { return []byte("1"), nil }
type Root struct { Value Custom ` + "`json:\"value\"`" + ` }
`,
			want: "custom JSON/text wire mappings are not statically derivable",
		},
		{
			name: "provider",
			body: `
type Root struct { Value string ` + "`json:\"value\"`" + ` }
func provide(string) json.Marshaler { return json.RawMessage(` + "`\"provided\"`" + `) }
`,
			want: "runtime schema provider with no statically resolved wire type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			option := ""
			if test.name == "provider" {
				option = ", polytype.WithFunction(Root{}.Value, provide)"
			}
			source := fmt.Sprintf(`//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

%s

func (Root) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Root.Schema%s)
`, test.body, option)
			builder := loadTypeGrammarFixture(t, source)
			_, err := builder.TypeDefinitions()
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestTypeDefinitionsSourceAdmissionRejectsInvalidCompositions(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		options string
		want    string
	}{
		{
			name: "cycle",
			body: `type Root struct { Next *Root ` + "`json:\"next\"`" + ` }`,
			want: "cyclic dependency",
		},
		{
			name: "map",
			body: `type Root struct { Values map[string]int ` + "`json:\"values\"`" + ` }`,
			want: "mapType/chanType not allowed",
		},
		{
			name: "wrapper in container",
			body: `type Root struct { Values []polytype.Optional[int] ` + "`json:\"values\"`" + ` }`,
			want: "supported only as the complete type of a direct named struct field",
		},
		{
			name: "interface implementation mismatch",
			body: `
type Event interface{ event(); Other() }
type Wrong struct{}
func (Wrong) event() {}
type Root struct { Event Event ` + "`json:\"event\"`" + ` }
`,
			want: "does not implement the complete interface",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf(`//go:build jsonschema

package fixture

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

%s

func (Root) Schema() json.RawMessage { panic("not implemented") }
var _ = polytype.NewJSONSchemaMethod(Root.Schema%s)
`, test.body, test.options)
			dir := writeTypeGrammarFixture(t, source)
			packages, err := syntax.Load(dir)
			require.NoError(t, err)
			require.Len(t, packages, 1)
			builder, err := New(packages[0])
			if err == nil {
				_, err = builder.TypeDefinitions()
			}
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestTypeDefinitionsFailsFastOnPackageErrors(t *testing.T) {
	dir := writeTypeGrammarFixture(t, `//go:build jsonschema

package fixture

var broken int = "not an int"
`)
	packages, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	require.NotEmpty(t, packages[0].Errors)
	builder := SchemaBuilder{Scan: syntax.ScanResult{Pkg: packages[0]}}
	_, err = builder.TypeDefinitions()
	require.ErrorContains(t, err, "has type-check errors")
}

// TestTypeDefinitionsIncludesFreeFunctionPointerRoot proves that a
// free-function schema root whose type's underlying type is a pointer (so
// it can't have a Schema method) still gets lowered into the TypeScript type
// grammar. TypeDefinitions used to walk SchemaMethods() only, silently
// dropping such roots from generated TypeScript even though they have a
// working JSON schema and Go accessor.
func TestTypeDefinitionsIncludesFreeFunctionPointerRoot(t *testing.T) {
	builder := loadTypeGrammarFixture(t, `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type PointerRoot *int

func PointerRootSchema(PointerRoot) json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(PointerRootSchema)
`)
	defs, err := builder.TypeDefinitions()
	require.NoError(t, err)
	requireDefinition(t, defs, "PointerRoot")
}

func loadTypeGrammarFixture(t *testing.T, source string) SchemaBuilder {
	t.Helper()
	dir := writeTypeGrammarFixture(t, source)
	packages, err := syntax.Load(dir)
	require.NoError(t, err)
	require.Len(t, packages, 1)
	require.Empty(t, packages[0].Errors)
	builder, err := New(packages[0])
	require.NoError(t, err)
	return builder
}

func writeTypeGrammarFixture(t *testing.T, source string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	dir := t.TempDir()
	module := fmt.Sprintf("module example.com/typegrammarfixture\n\ngo 1.27\n\nrequire github.com/tylergannon/polytype v0.0.0\nreplace github.com/tylergannon/polytype => %s\n", root)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(source), 0o644))
	return dir
}

func requireDefinition(t *testing.T, defs typegrammar.Definitions, name string) typegrammar.Definition {
	t.Helper()
	for _, def := range defs {
		if def.Name.Name == name {
			return def
		}
	}
	require.FailNow(t, "missing definition", name)
	return typegrammar.Definition{}
}

func requireField(t *testing.T, fields []typegrammar.Field, jsonName string) typegrammar.Field {
	t.Helper()
	for _, field := range fields {
		if field.JSONName == jsonName {
			return field
		}
	}
	require.FailNow(t, "missing field", jsonName)
	return typegrammar.Field{}
}

func fieldJSONNames(fields []typegrammar.Field) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.JSONName)
	}
	return names
}

func enumMemberNames(members []typegrammar.EnumMember) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Name)
	}
	return names
}

func enumMemberValues(members []typegrammar.EnumMember) []string {
	values := make([]string, 0, len(members))
	for _, member := range members {
		values = append(values, member.Value.ExactString())
	}
	return values
}

func fieldType[T typegrammar.Type](t *testing.T, value typegrammar.FieldValue) T {
	t.Helper()
	var typ typegrammar.Type
	switch field := value.(type) {
	case *typegrammar.Required:
		typ = field.Type
	case *typegrammar.Optional:
		typ = field.Type
	case *typegrammar.Nullable:
		typ = field.Type
	default:
		require.FailNow(t, "field does not contain an ordinary type", fmt.Sprintf("%T", value))
	}
	result, ok := typ.(T)
	require.True(t, ok, "got %T", typ)
	return result
}
