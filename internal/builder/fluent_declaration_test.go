package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/syntax"
)

// writeFluentFixture writes a single-file package to a fresh temp directory
// under testfixtures/ and builds it in-process (no go.mod/go generate),
// mirroring writeInlineInterfaceFixture's pattern.
func writeFluentFixture(t *testing.T, source string) SchemaBuilder {
	t.Helper()

	cwd, err := os.Getwd()
	require.NoError(t, err)
	targetDir, err := os.MkdirTemp(filepath.Join(cwd, "testfixtures"), "fluent_")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(targetDir))
	})

	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "schema.go"), []byte(source), 0o644))

	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	builder, err := New(pkgs[0])
	require.NoError(t, err)
	return builder
}

// jsonFor renders a built type's JSON schema through the same
// marshalSchemaHardlines path used for generated .json/.json.tmpl files, so
// legacy and fluent registrations of the "same" type can be compared for
// byte-for-byte parity.
func jsonFor(t *testing.T, b SchemaBuilder, typeName string) string {
	t.Helper()
	schema, ok := b.schemas.Get(b.Scan.Pkg.PkgPath, typeName)
	require.True(t, ok, "no schema recorded for %s", typeName)
	out, err := marshalSchemaHardlines(schema)
	require.NoError(t, err)
	return string(out)
}

const fluentProviderFixture = `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
	C bool   ` + "`json:\"c\"`" + `
}

func (Example) Schema() json.RawMessage { panic("not implemented") }
func (Example) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (Example) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}
func BoolSchemaFunc(_ bool) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"boolean\",\"description\":\"C\"}`" + `)
}

var _ = %s
`

const legacyProviderRegistration = `polytype.NewJSONSchemaMethod(
	Example.Schema,
	polytype.WithStructAccessorMethod(Example{}.A, (Example).ASchema),
	polytype.WithStructFunctionMethod(Example{}.B, (Example).BSchema),
	polytype.WithFunction(Example{}.C, BoolSchemaFunc),
	polytype.WithRenderProviders(),
)`

const fluentProviderRegistration = `polytype.Declare(Example.Schema).
	Accessor(polytype.Field[Example]("A"), Example.ASchema).
	Method(polytype.Field[Example]("B"), Example.BSchema).
	Function(polytype.Field[Example]("C"), BoolSchemaFunc).
	RenderProviders()`

// TestFluentProviderParityWithLegacy proves that Accessor/Method/Function/
// RenderProviders fluent chaining produces the exact same rendered schema as
// the equivalent NewJSONSchemaMethod + WithXxx registration, through the
// same builder path used for real generation.
func TestFluentProviderParityWithLegacy(t *testing.T) {
	t.Parallel()

	legacy := writeFluentFixture(t, fmt.Sprintf(fluentProviderFixture, legacyProviderRegistration))
	fluent := writeFluentFixture(t, fmt.Sprintf(fluentProviderFixture, fluentProviderRegistration))

	require.Equal(t, jsonFor(t, legacy, "Example"), jsonFor(t, fluent, "Example"))
}

const fluentPointerProviderFixture = `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct {
	A string ` + "`json:\"a\"`" + `
	B int    ` + "`json:\"b\"`" + `
}

func (*Example) Schema() json.RawMessage { panic("not implemented") }
func (*Example) ASchema() json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"string\",\"description\":\"A\"}`" + `)
}
func (*Example) BSchema(_ int) json.Marshaler {
	return json.RawMessage(` + "`{\"type\":\"integer\",\"description\":\"B\"}`" + `)
}

var _ = %s
`

const legacyPointerProviderRegistration = `polytype.NewJSONSchemaMethod(
	(*Example).Schema,
	polytype.WithStructAccessorMethod(Example{}.A, (*Example).ASchema),
	polytype.WithStructFunctionMethod(Example{}.B, (*Example).BSchema),
)`

const fluentPointerProviderRegistration = `polytype.Declare((*Example).Schema).
	Accessor(Example{}.A, (*Example).ASchema).
	Method(Example{}.B, (*Example).BSchema)`

// TestFluentPointerRootProviderParityWithLegacy proves that a pointer-root
// fluent chain (Declare((*T).Schema).Accessor/.Method with pointer method
// expressions) actually retains its providers, matching the equivalent
// pointer-receiver legacy registration byte-for-byte. This reproduces the
// issue #73 review finding: providerRef previously rejected the *dst.StarExpr
// inside "(*Example).ASchema" and silently dropped the provider option.
func TestFluentPointerRootProviderParityWithLegacy(t *testing.T) {
	t.Parallel()

	legacy := writeFluentFixture(t, fmt.Sprintf(fluentPointerProviderFixture, legacyPointerProviderRegistration))
	fluent := writeFluentFixture(t, fmt.Sprintf(fluentPointerProviderFixture, fluentPointerProviderRegistration))

	legacyJSON := jsonFor(t, legacy, "Example")
	require.Equal(t, legacyJSON, jsonFor(t, fluent, "Example"))
	require.Contains(t, legacyJSON, `{{.a}}`)
	require.Contains(t, legacyJSON, `{{.b}}`)
}

const enumMarkerFixture = `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

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

var _ = polytype.Declare(Widget.Schema).
	StringerEnum(Widget{}.ViaStringer)
`

// TestEnumMarkerEmitsConstantValuesAndIgnoresStringer proves that a type
// declaring func (T) enum() is emitted as an enum of its typed constants
// with no field-level declaration, that a String() method on the marked
// type does not change the wire values, and that an explicit .StringerEnum
// on a field of the marked type still selects name mode for that field.
func TestEnumMarkerEmitsConstantValuesAndIgnoresStringer(t *testing.T) {
	t.Parallel()

	builder := writeFluentFixture(t, enumMarkerFixture)
	rendered := jsonFor(t, builder, "Widget")
	require.Contains(t, rendered, `"direct":{"type":"string","enum":["red","green"]}`)
	require.Contains(t, rendered, `"level":{"type":"integer","enum":[1,2]}`)
	require.Contains(t, rendered, `"viaStringer":{"type":"string","enum":["Low","High"]}`)
}

const fluentRefFixture = `//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Shared struct {
	Name string ` + "`json:\"name\"`" + `
}

func (Shared) Schema() json.RawMessage { panic("not implemented") }

type Owner struct {
	Value Shared ` + "`json:\"value\"`" + `
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = %s
var _ = polytype.NewJSONSchemaMethod(Owner.Schema)
`

const legacyRefRegistration = `polytype.NewJSONSchemaMethod(Shared.Schema, polytype.AsRef())`
const fluentRefRegistration = `polytype.Declare(Shared.Schema).Ref()`

// TestFluentRefParityWithLegacy proves that .Ref() produces the same
// "$ref"-based owner schema as AsRef().
func TestFluentRefParityWithLegacy(t *testing.T) {
	t.Parallel()

	legacy := writeFluentFixture(t, fmt.Sprintf(fluentRefFixture, legacyRefRegistration))
	fluent := writeFluentFixture(t, fmt.Sprintf(fluentRefFixture, fluentRefRegistration))

	require.Equal(t, jsonFor(t, legacy, "Owner"), jsonFor(t, fluent, "Owner"))
	require.Contains(t, jsonFor(t, legacy, "Owner"), `"$ref"`)
}

// TestFluentAccessorRejectsFreeFunctionProvider proves the issue #73 review
// finding is fixed: passing a free function shaped like func(T)
// json.Marshaler to .Accessor (where Go's type system accepts it identically
// to the intended receiver method expression) is now a source-positioned
// scanner error, not a silently-produced FieldProvider that the code-gen
// template has no branch for and that panics at runtime.
func TestFluentAccessorRejectsFreeFunctionProvider(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	require.NoError(t, err)
	targetDir, err := os.MkdirTemp(filepath.Join(cwd, "testfixtures"), "fluent_accessor_shape_")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(targetDir))
	})

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
	Accessor(Example{}.A, freeAccessorSchema).
	RenderProviders()
`
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "schema.go"), []byte(source), 0o644))

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

	cwd, err := os.Getwd()
	require.NoError(t, err)
	targetDir, err := os.MkdirTemp(filepath.Join(cwd, "testfixtures"), "fluent_function_shape_")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(targetDir))
	})

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
	Function(Example{}.H, Passthrough.PassthroughSchema).
	RenderProviders()
`
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "schema.go"), []byte(source), 0o644))

	pkgs, err := syntax.Load(targetDir)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.Empty(t, pkgs[0].Errors)

	_, err = New(pkgs[0])
	require.ErrorContains(t, err, "polytype.Declare: .Function provider is not a supported method expression or free function")
	require.ErrorContains(t, err, "schema.go")
}
