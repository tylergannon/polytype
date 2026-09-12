package indirecttypes

import "github.com/tylergannon/polytype"

//go:generate go run ../../polytype/

// This example demonstrates various forms of indirection in type definitions
// including pointers, slices, and type aliases.

// SimpleInt is a basic integer type.
type SimpleInt int

// PointerToInt demonstrates a pointer to a basic type.
// In the schema, this will be the same as the base type but nullable.
type PointerToInt *int

// PointerToSimpleInt demonstrates a pointer to a custom type.
// In the schema, this will be the same as SimpleInt but nullable.
type PointerToSimpleInt *SimpleInt

// SliceOfInt demonstrates a slice of a basic type.
// In the schema, this will be an array of integers.
type SliceOfInt []int

// SliceOfSimpleInt demonstrates a slice of a custom type.
// In the schema, this will be an array of SimpleInt.
type SliceOfSimpleInt []SimpleInt

// SliceOfPointerToInt demonstrates a slice of pointers to a basic type.
// In the schema, this will be an array of nullable integers.
type SliceOfPointerToInt []*int

// SliceOfPointerToSimpleInt demonstrates a slice of pointers to a custom type.
// In the schema, this will be an array of nullable SimpleInt.
type SliceOfPointerToSimpleInt []*SimpleInt

// NamedSliceType demonstrates naming a slice type.
// This creates another level of indirection.
type NamedSliceType SliceOfSimpleInt

// NestedTypes demonstrates deeply nested types.
// Person is a basic struct with simple fields.
type Person struct {
	// Name is the person's name.
	Name string `json:"name"`

	// Age is the person's age.
	Age int `json:"age"`
}

// PointerToPerson is a pointer to a Person.
// In the schema, this will be the same as Person but nullable.
type PointerToPerson *Person

// SliceOfPerson is a slice of Person objects.
// In the schema, this will be an array of Person objects.
type SliceOfPerson []Person

// SliceOfPointerToPerson is a slice of pointers to Person objects.
// In the schema, this will be an array of nullable Person objects.
type SliceOfPointerToPerson []*Person

// COMMENTED OUT: Map types are not yet supported by the generator
// // MapOfStringToPerson is a map with string keys and Person values.
// // In the schema, this will be an object with string keys and Person values.
// type MapOfStringToPerson map[string]Person

// // MapOfStringToPointerToPerson is a map with string keys and Person pointer values.
// // In the schema, this will be an object with string keys and nullable Person values.
// type MapOfStringToPointerToPerson map[string]*Person

// ComplexStruct demonstrates using all these indirect types in a struct.
type ComplexStruct struct {
	// SimpleValue is a simple integer.
	SimpleValue SimpleInt `json:"simpleValue"`

	// PointerValue is a nullable integer.
	PointerValue polytype.Nullable[SimpleInt] `json:"pointerValue"`

	// SliceValue is an array of SimpleInt.
	SliceValue SliceOfSimpleInt `json:"sliceValue"`

	// PointerSliceValue is an array of nullable SimpleInt.
	PointerSliceValue SliceOfPointerToSimpleInt `json:"pointerSliceValue"`

	// NamedSliceValue demonstrates using a named slice type.
	NamedSliceValue NamedSliceType `json:"namedSliceValue"`

	// PersonValue is a Person object.
	PersonValue Person `json:"personValue"`

	// PersonPointerValue is a nullable Person object.
	PersonPointerValue polytype.Nullable[*Person] `json:"personPointerValue"`

	// PeopleValue is an array of Person objects.
	PeopleValue SliceOfPerson `json:"peopleValue"`

	// PeoplePointerValue is an array of nullable Person objects.
	PeoplePointerValue SliceOfPointerToPerson `json:"peoplePointerValue"`

	// COMMENTED OUT: Map types are not yet supported
	// // PeopleMapValue is an object with string keys and Person values.
	// PeopleMapValue MapOfStringToPerson `json:"peopleMapValue,omitempty"`

	// // PeoplePointerMapValue is an object with string keys and nullable Person values.
	// PeoplePointerMapValue MapOfStringToPointerToPerson `json:"peoplePointerMapValue,omitempty"`
}
