package polytype

import (
	"encoding/json"
)

type (
	SchemaMarker struct{}

	SchemaFunction func() json.RawMessage

	SchemaMethod[T any] func(T) json.RawMessage
)

// WithRenderProviders requests generation of RenderedSchema() and provider execution at runtime.
//
// Deprecated: use Declare(T.Schema).RenderProviders() instead.
func WithRenderProviders() SchemaMethodOption { return SchemaMethodOptionObj{} }

// AsRef requests that, wherever this type is referenced from another
// registered schema, it be rendered as a "$ref" into that schema's "$defs"
// instead of being inlined.
//
// Deprecated: use Declare(T.Schema).Ref() instead.
func AsRef() SchemaMethodOption { return SchemaMethodOptionObj{} }

// NewJSONSchemaBuilder registers a function as being a stub that should be
// implemented with a proper json schema and, as needed, unmarshaler functionality.
func NewJSONSchemaBuilder[T any](SchemaFunction) SchemaMarker {
	return SchemaMarker{}
}

type SchemaMethodOption interface {
	implementsSchemaMethodOption()
}

type exampleStruct struct {
	Field1 string
	Field2 int
	Field3 bool
}

func buildBoolSchema(val bool) json.Marshaler {
	return json.RawMessage(`{"type": "boolean"}`)
}

func (exampleStruct) field1Schema() json.Marshaler {
	return json.RawMessage(`{"type": "string"}`)
}

func (exampleStruct) field2Schema(int) json.Marshaler {
	return json.RawMessage(`{"type": "integer"}`)
}

func (exampleStruct) JSONSchema() json.RawMessage {
	panic("not implemented")
}

// Deprecated: use Declare(T.Schema).Function(Field[T, F]("Field"), fn) instead.
func WithFunction[T any](val T, f func(T) json.Marshaler) SchemaMethodOption {
	return SchemaMethodOptionObj{}
}

// Deprecated: use Declare(T.Schema).Method(Field[T, F]("Field"), T.method) instead.
func WithStructFunctionMethod[T, U any](val U, f func(T, U) json.Marshaler) SchemaMethodOption {
	return SchemaMethodOptionObj{}
}

// Deprecated: use Declare(T.Schema).Accessor(Field[T, F]("Field"), T.method) instead.
func WithStructAccessorMethod[T, U any](val T, f func(U) json.Marshaler) SchemaMethodOption {
	return SchemaMethodOptionObj{}
}

type SchemaMethodOptionObj struct{}

func (SchemaMethodOptionObj) implementsSchemaMethodOption() {}

// Enum options (v1) - stubs for scanning/type-checking; parsed by scanner
//
// Deprecated: use Declare(T.Schema).StringerEnum(Field[T, F]("Field")) instead.
func WithStringerEnum[T any](field T) SchemaMethodOption { return SchemaMethodOptionObj{} }

// NewJSONSchemaMethod registers a struct method as a stub that will be implemented
// with a proper json schema and, as needed, unmarshaler functionality.
//
// Deprecated: use Declare(T.Schema) instead. For example,
// NewJSONSchemaMethod(Task.Schema, WithStringerEnum(Task{}.Level)) becomes
// Declare(Task.Schema).StringerEnum(Field[Task, Level]("Level")).
func NewJSONSchemaMethod[T any](SchemaMethod[T], ...SchemaMethodOption) SchemaMarker {
	return SchemaMarker{}
}

// NewJSONSchemaFunc registers a free function that takes the receiver as its
// sole parameter as a schema entrypoint. It is equivalent to NewJSONSchemaMethod.
//
// Deprecated: use Declare(fn) with a free function instead.
func NewJSONSchemaFunc[T any](f SchemaMethod[T], _ ...SchemaMethodOption) SchemaMarker { // options parsed by scanner only for now
	_ = f
	return SchemaMarker{}
}

var _ SchemaMarker = NewJSONSchemaMethod(
	exampleStruct.JSONSchema,
	WithStructAccessorMethod(exampleStruct{}.Field1, exampleStruct.field1Schema),
	WithStructFunctionMethod(exampleStruct{}.Field2, exampleStruct.field2Schema),
	WithFunction(exampleStruct{}.Field3, buildBoolSchema),
)
