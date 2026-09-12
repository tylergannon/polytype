// Package typegrammar defines the accepted, resolved static type-definition
// grammar shared by code-generation backends. Validate is its admission boundary.
//
// A definition describes a Go type's JSON value structure, retaining numeric
// kinds, pointer/value identity, collection shape, and field-local registrations.
// Source loading must resolve aliases, embedding/field selection, registrations,
// and JSON names before constructing this model. The builder's TypeDefinitions
// adapter produces this model for the TypeScript backend.
//
// Types form a finite DAG. References may share definitions but may not introduce
// recursion. Objects are closed, ordered sets of properties. Ordinary values are
// non-null; absence and null are separate, direct-field constructors. Unions are
// field-only constructors with explicit, resolved tags, including singleton
// unions. There is no general anyOf, any, map, or opaque-provider constructor.
//
// This is the static structural subset of the v1 contract, not a Go-source
// parser, arbitrary JSON Schema grammar, or claim of codec conformance. Runtime
// provider output, unresolved external schema refs and unproved custom wire
// mappings must be diagnosed by lowering, not replaced with a permissive node.
// Backends must define their projection explicitly: for example TypeScript's
// number cannot enforce all Go ranges, and its object types are not validators.
//
// New node kinds may be added in minor versions. Consumers must not treat a
// type switch over the node types as exhaustive: always provide a default
// case that reports an unrecognized kind rather than silently ignoring it.
package typegrammar

import (
	"go/constant"
	"go/token"
)

// Name identifies a resolved Go type. PackagePath is always a full import path;
// pointer qualification and field-specific wire identity do not belong here.
type Name struct {
	PackagePath string
	Name        string
}

func (n Name) String() string { return n.PackagePath + "." + n.Name }

// Definitions is the complete local definition graph, in source order. Every
// reference and union implementation must resolve here, including dependencies
// from supported packages. Consumers select output roots separately.
type Definitions []Definition

type Definition struct {
	Name        Name
	Type        Type
	Description string
	Source      token.Position
}

// Type is the closed set of ordinary, non-null type constructors. Only pointer
// nodes implement it, allowing validation to detect cycles in hand-built graphs
// as well as cycles through named references. A node is admitted only after the
// entire Definitions value passes Validate; Go structs alone cannot enforce
// graph or context invariants. Do not mutate admitted graphs during consumption.
type Type interface{ typeNode() }

// ScalarKind retains Go numeric width and signedness instead of prematurely
// projecting all JSON numbers to float64. Int and Uint remain platform-sized.
// byte/rune aliases normalize to Uint8/Int32. Complex and uintptr are excluded.
type ScalarKind string

const (
	Bool    ScalarKind = "bool"
	String  ScalarKind = "string"
	Int     ScalarKind = "int"
	Int8    ScalarKind = "int8"
	Int16   ScalarKind = "int16"
	Int32   ScalarKind = "int32"
	Int64   ScalarKind = "int64"
	Uint    ScalarKind = "uint"
	Uint8   ScalarKind = "uint8"
	Uint16  ScalarKind = "uint16"
	Uint32  ScalarKind = "uint32"
	Uint64  ScalarKind = "uint64"
	Float32 ScalarKind = "float32"
	Float64 ScalarKind = "float64"
)

type Scalar struct{ Kind ScalarKind }

// Time is the renderer-owned time.Time leaf. Its wire type is string; neither
// date-time schema validation nor JavaScript Date precision is implied.
type Time struct{}

// Enum is a resolved registration, not a global adapter for GoType. The same Go
// type can have different mappings in different fields. Values retain exact Go
// constants, including integers not exactly representable by JavaScript number.
// Representability in a target language is a later backend obligation.
type Enum struct {
	GoType  Name
	Kind    ScalarKind
	Mode    EnumMode
	Members []EnumMember
}

type EnumMode uint8

const (
	// EnumValues uses underlying string or integer constant values on the wire.
	EnumValues EnumMode = iota
	// EnumNames uses Go constant names as wire strings, for integer enums only.
	// It is admitted only in direct fields of named object owners (Required,
	// Optional or Nullable), including references to reusable enum definitions.
	// Pointer/collection operands and anonymous owners cannot request this
	// adaptation. String() methods never determine wire values.
	EnumNames
)

type EnumMember struct {
	Name  string
	Value constant.Value
}

// Object's properties retain resolved Go field order and are closed to unknown
// JSON properties. An empty object is valid. Embedding is resolved before here.
type Object struct{ Fields []Field }

// Pointer retains source indirection without introducing null into the grammar.
type Pointer struct{ Element Type }

// Slice represents an ordinary non-null slice. Byte-like slices, whose default
// Go JSON representation is base64, are outside this static portable grammar.
type Slice struct{ Element Type }

// Array retains the Go length, including zero. It does not by itself claim that
// the existing JSON Schema renderer enforces that length.
type Array struct {
	Length  int64
	Element Type
}

// Ref is a resolved named-type edge, not an arbitrary URI or a request for a
// particular backend's reference syntax (such as JSON Schema's AsRef option).
type Ref struct{ Target Name }

func (*Scalar) typeNode()  {}
func (*Time) typeNode()    {}
func (*Enum) typeNode()    {}
func (*Object) typeNode()  {}
func (*Pointer) typeNode() {}
func (*Slice) typeNode()   {}
func (*Array) typeNode()   {}
func (*Ref) typeNode()     {}

type Field struct {
	GoName      string
	JSONName    string
	Value       FieldValue
	Description string
	Source      token.Position
}

// FieldValue is a closed sum of direct-field forms. In particular a Union is
// not a Type: refs, pointers, aliases and ordinary collections cannot hide one.
// A collection of named objects whose own fields contain unions is permitted.
type FieldValue interface{ fieldValue() }

// Required is a required non-null field. Direct pointers and ordinary
// omitempty/omitzero tags are rejected during lowering; authors must state
// nullability or omission with Nullable or Optional.
type Required struct{ Type Type }

// Optional is absent or a non-null value, corresponding to direct Optional[T].
// Lowering must check the required json:",omitzero" tag. It cannot occur inside
// collections or definitions because it does not implement Type.
type Optional struct{ Type Type }

// Nullable is a required key that admits null or a value. Its admitted operands
// are scalars/enums, objects, pointers to objects, and refs to those shapes;
// time.Time is also supported; collection and union operands are excluded.
type Nullable struct{ Type Type }

// Union describes a direct registered interface field I. Its identity is the
// containing definition/field, not Interface alone. Tags have already been
// resolved from either explicit strings (including empty strings) or legacy
// implementation names. Discriminator is the effective, nonempty property name
// after applying registration defaults; whitespace is never trimmed here.
// Every member is a non-null object carrying the required Discriminator key
// with its exact Tag. A pointer registration does not imply nullable payloads.
type Union struct {
	Interface     Name
	Discriminator string
	Variants      []Variant
}

// OptionalUnion and UnionSlice are the only other admitted interface forms:
// direct Optional[I] and direct one-dimensional []I. Neither is a Type.
type OptionalUnion struct{ Union Union }
type UnionSlice struct{ Union Union }

type Variant struct {
	Implementation Name
	Pointer        bool
	Tag            string
	Source         token.Position
}

func (*Required) fieldValue()      {}
func (*Optional) fieldValue()      {}
func (*Nullable) fieldValue()      {}
func (*Union) fieldValue()         {}
func (*OptionalUnion) fieldValue() {}
func (*UnionSlice) fieldValue()    {}
