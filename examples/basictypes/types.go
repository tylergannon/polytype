package basictypes

import "github.com/tylergannon/polytype"

//go:generate go run ../../polytype/

// SimpleInt demonstrates a basic integer type that will be represented
// as a number in the JSON schema.
// The description from this comment will be included in the schema.
type SimpleInt int

// SimpleString demonstrates a basic string type that will be represented
// as a string in the JSON schema.
type SimpleString string

// SimpleFloat demonstrates a basic float type that will be represented
// as a number in the JSON schema.
type SimpleFloat float64

// SimpleStruct demonstrates a basic struct with primitive fields.
// Each field can have its own documentation which will be included
// in the schema.
type SimpleStruct struct {
	// ID is a unique identifier for this struct.
	// This comment will be included in the schema description for this field.
	ID SimpleInt `json:"id"`

	// Name is the display name.
	// Multiline comments are supported and will be preserved in the schema.
	Name SimpleString `json:"name"`

	// Score is a numerical value between 0 and 100.
	Score SimpleFloat `json:"score"`

	// Tags are additional metadata for this struct.
	Tags polytype.Optional[[]string] `json:"tags,omitzero"`

	// InternalID is not exposed in JSON.
	InternalID string `json:"-"`
}

// NestedTypeDeclaration demonstrates how multiple types can be declared
// in a single type block, which is a common Go pattern.
type (
	// TypeInNestedDecl is a type declared in a group.
	TypeInNestedDecl int

	// AnotherNestedType is another type in the same declaration block.
	AnotherNestedType string
)
