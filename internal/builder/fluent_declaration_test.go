package builder

import (
	"go/constant"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// writeFluentFixture writes a single-file package to a fresh temp module and
// builds it in process (no go.mod/go generate of its own).
func writeFluentFixture(t *testing.T, source string) SchemaBuilder {
	t.Helper()
	return loadBuilder(t, newFixture(t, map[string]string{"schema.go": source}))
}

// fluentParityFixture registers every shape twice, once through the
// deprecated NewJSONSchemaMethod/With* form and once through Declare, so one
// load proves each fluent option lowers exactly like its legacy equivalent:
// Accessor/Method/Function providers with RenderProviders on a value root,
// the same providers on a pointer root, and Ref. It also carries the
// enum-marker shapes, which need no registration at all.
const fluentParityFixture = `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Legacy struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
	C bool   ` + "`json:\"c\"`" + `
}

func (Legacy) Schema() json.RawMessage { panic("not implemented") }
func (Legacy) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (Legacy) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}

type Fluent struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
	C bool   ` + "`json:\"c\"`" + `
}

func (Fluent) Schema() json.RawMessage { panic("not implemented") }
func (Fluent) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (Fluent) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}

func BoolSchemaFunc(_ bool) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"boolean\",\"description\":\"C\"}`" + `)
}

type LegacyPointer struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
}

func (*LegacyPointer) Schema() json.RawMessage { panic("not implemented") }
func (*LegacyPointer) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (*LegacyPointer) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}

type FluentPointer struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
}

func (*FluentPointer) Schema() json.RawMessage { panic("not implemented") }
func (*FluentPointer) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (*FluentPointer) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}

type LegacyShared struct {
	Name string ` + "`json:\"name\"`" + `
}

func (LegacyShared) Schema() json.RawMessage { panic("not implemented") }

type FluentShared struct {
	Name string ` + "`json:\"name\"`" + `
}

func (FluentShared) Schema() json.RawMessage { panic("not implemented") }

type LegacyOwner struct {
	Value LegacyShared ` + "`json:\"value\"`" + `
}

func (LegacyOwner) Schema() json.RawMessage { panic("not implemented") }

type FluentOwner struct {
	Value FluentShared ` + "`json:\"value\"`" + `
}

func (FluentOwner) Schema() json.RawMessage { panic("not implemented") }

type Paint string

func (Paint) enum() {}

const (
	Red   Paint = "red"
	Green Paint = "green"
)

// String is ignored for a marked enum: the marker means value mode.
func (p Paint) String() string { return "not-the-wire-value" }

type Level int

func (Level) enum() {}

const (
	Low  Level = 1
	High Level = 2
)

type Widget struct {
	Direct      Paint ` + "`json:\"direct\"`" + `
	Level       Level ` + "`json:\"level\"`" + `
	ViaStringer Level ` + "`json:\"viaStringer\"`" + `
}

func (Widget) Schema() json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewJSONSchemaMethod(
		Legacy.Schema,
		polytype.WithStructAccessorMethod(Legacy{}.A, (Legacy).ASchema),
		polytype.WithStructFunctionMethod(Legacy{}.B, (Legacy).BSchema),
		polytype.WithFunction(Legacy{}.C, BoolSchemaFunc),
		polytype.WithRenderProviders(),
	)
	_ = polytype.Declare(Fluent.Schema).
		Accessor(polytype.Field[Fluent, string]("A"), Fluent.ASchema).
		Method(polytype.Field[Fluent, int]("B"), Fluent.BSchema).
		Function(polytype.Field[Fluent, bool]("C"), BoolSchemaFunc).
		RenderProviders()
	_ = polytype.NewJSONSchemaMethod(
		(*LegacyPointer).Schema,
		polytype.WithStructAccessorMethod(LegacyPointer{}.A, (*LegacyPointer).ASchema),
		polytype.WithStructFunctionMethod(LegacyPointer{}.B, (*LegacyPointer).BSchema),
	)
	_ = polytype.Declare((*FluentPointer).Schema).
		Accessor(polytype.Field[FluentPointer, string]("A"), (*FluentPointer).ASchema).
		Method(polytype.Field[FluentPointer, int]("B"), (*FluentPointer).BSchema)
	_ = polytype.NewJSONSchemaMethod(LegacyShared.Schema, polytype.AsRef())
	_ = polytype.Declare(FluentShared.Schema).Ref()
	_ = polytype.NewJSONSchemaMethod(LegacyOwner.Schema)
	_ = polytype.NewJSONSchemaMethod(FluentOwner.Schema)
	_ = polytype.Declare(Widget.Schema).
		StringerEnum(polytype.Field[Widget, Level]("ViaStringer"))
)
`

func fieldValues(object *typegrammar.Object) []typegrammar.FieldValue {
	values := make([]typegrammar.FieldValue, 0, len(object.Fields))
	for _, field := range object.Fields {
		values = append(values, field.Value)
	}
	return values
}

func refTypeNames(b SchemaBuilder) []string {
	var names []string
	for id := range b.RefTypes {
		names = append(names, id.TypeName)
	}
	return names
}

// TestFluentDeclarationParityWithLegacy proves the fluent chain lowers to
// the same definitions and resolves the same provider table as the
// equivalent legacy registration, through the one builder path real
// generation uses. The pointer-root case reproduces the issue #73 review
// finding: providerRef previously rejected the *dst.StarExpr inside
// "(*Example).ASchema" and silently dropped the provider option.
func TestFluentDeclarationParityWithLegacy(t *testing.T) {
	t.Parallel()
	builder := writeFluentFixture(t, fluentParityFixture)

	t.Run("providers", func(t *testing.T) {
		legacy, fluent := loweredObject(t, builder, "Legacy"), loweredObject(t, builder, "Fluent")
		require.Equal(t, []typegrammar.FieldValue{&typegrammar.Provided{}, &typegrammar.Provided{}, &typegrammar.Provided{}}, fieldValues(legacy))
		require.Equal(t, fieldValues(legacy), fieldValues(fluent))
		require.Len(t, builder.TypeProvidersMap["Legacy"], 3)
		require.Equal(t, builder.TypeProvidersMap["Legacy"], builder.TypeProvidersMap["Fluent"])
		require.True(t, builder.Rendered["Legacy"])
		require.True(t, builder.Rendered["Fluent"])
	})

	t.Run("pointer root providers", func(t *testing.T) {
		legacy, fluent := loweredObject(t, builder, "LegacyPointer"), loweredObject(t, builder, "FluentPointer")
		require.Equal(t, []typegrammar.FieldValue{&typegrammar.Provided{}, &typegrammar.Provided{}}, fieldValues(legacy))
		require.Equal(t, fieldValues(legacy), fieldValues(fluent))
		require.Len(t, builder.TypeProvidersMap["LegacyPointer"], 2)
		require.Equal(t, builder.TypeProvidersMap["LegacyPointer"], builder.TypeProvidersMap["FluentPointer"])
		require.False(t, builder.Rendered["LegacyPointer"])
		require.False(t, builder.Rendered["FluentPointer"])
	})

	t.Run("ref", func(t *testing.T) {
		require.ElementsMatch(t, []string{"LegacyShared", "FluentShared"}, refTypeNames(builder))
		for _, owner := range []string{"LegacyOwner", "FluentOwner"} {
			value := requireField(t, loweredObject(t, builder, owner).Fields, "value")
			require.IsType(t, &typegrammar.Ref{}, fieldType[typegrammar.Type](t, value.Value), owner)
		}
	})

	// A type declaring func (T) enum() is an enum of its typed constants with
	// no field-level declaration; a String() method on the marked type does
	// not change the wire values; and an explicit .StringerEnum on a field
	// of the marked type still selects name mode for that field.
	t.Run("enum marker", func(t *testing.T) {
		widget := loweredObject(t, builder, "Widget")
		direct := loweredEnum(t, builder, requireField(t, widget.Fields, "direct").Value)
		require.Equal(t, typegrammar.EnumValues, direct.Mode)
		require.Equal(t, []string{"red", "green"}, enumStringValues(direct.Members))
		level := loweredEnum(t, builder, requireField(t, widget.Fields, "level").Value)
		require.Equal(t, typegrammar.EnumValues, level.Mode)
		require.Equal(t, []string{"1", "2"}, enumExactValues(level.Members))
		viaStringer := loweredEnum(t, builder, requireField(t, widget.Fields, "viaStringer").Value)
		require.Equal(t, typegrammar.EnumNames, viaStringer.Mode)
		require.Equal(t, []string{"Low", "High"}, enumMemberNames(viaStringer.Members))
	})
}

func enumStringValues(members []typegrammar.EnumMember) []string {
	values := make([]string, 0, len(members))
	for _, member := range members {
		values = append(values, constant.StringVal(member.Value))
	}
	return values
}

func enumExactValues(members []typegrammar.EnumMember) []string {
	values := make([]string, 0, len(members))
	for _, member := range members {
		values = append(values, member.Value.ExactString())
	}
	return values
}

// TestFluentAccessorRejectsFreeFunctionProvider proves the issue #73 review
// finding is fixed: passing a free function shaped like func(T)
// json.Marshaler to .Accessor (where Go's type system accepts it identically
// to the intended receiver method expression) is now a source-positioned
// scanner error, not a silently-produced FieldProvider that the code-gen
// template has no branch for and that panics at runtime.
func TestFluentAccessorRejectsFreeFunctionProvider(t *testing.T) {
	t.Parallel()

	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct {
	A string ` + "`json:\"a\"`" + `
}

func (Example) Schema() json.RawMessage { panic("not implemented") }

func freeAccessorSchema(Example) json.Marshaler { panic("not implemented") }

var _ = polytype.Declare(Example.Schema).
	Accessor(polytype.Field[Example, string]("A"), freeAccessorSchema).
	RenderProviders()
`
	targetDir := newFixture(t, map[string]string{
		"schema.go": source,
	})

	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	_, err = New(pkgs[0])
	require.ErrorContains(t, err, "polytype.Declare: .Accessor provider must be a Example method expression, not a free function")
	require.ErrorContains(t, err, "schema.go")
}

// TestFluentFunctionRejectsUnrelatedMethodExpressionProvider proves the
// second reachable path to the same review finding: providerRef reports
// matched=false (rather than isMethod=true) when a chain link's provider is
// a method expression whose receiver isn't recognized against this chain
// (e.g. a method on the field's own type, passed to .Function, which
// requires a free function). Go's type system accepts this because
// .Function's provider signature is func(F) json.Marshaler, where F is the
// field's own type rather than the Declare root - so the receiver mismatch
// can only be caught here, and must be a hard error rather than the silent
// option-drop the "matched=false" case previously fell through to.
func TestFluentFunctionRejectsUnrelatedMethodExpressionProvider(t *testing.T) {
	t.Parallel()

	source := `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Passthrough struct{}

func (Passthrough) PassthroughSchema() json.Marshaler { panic("not implemented") }

type Example struct {
	H Passthrough ` + "`json:\"h\"`" + `
}

func (Example) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Example.Schema).
	Function(polytype.Field[Example, Passthrough]("H"), Passthrough.PassthroughSchema).
	RenderProviders()
`
	targetDir := newFixture(t, map[string]string{
		"schema.go": source,
	})

	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	_, err = New(pkgs[0])
	require.ErrorContains(t, err, "polytype.Declare: .Function provider is not a supported method expression or free function")
	require.ErrorContains(t, err, "schema.go")
}
