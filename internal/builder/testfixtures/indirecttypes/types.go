package basictypes

import (
	"github.com/tylergannon/polytype/internal/builder/testfixtures/indirecttypes/indirectsubpkg"
)

// IntType is Foobarbax
type IntType int

// PointerToIntType is Footballbat
type PointerToIntType *int

// PointerToIntType is a pointer to a named type
type PointerToNamedType *IntType

// DefinedAsNamedType is defined as another named type
type DefinedAsNamedType IntType

// SliceOfPointerToInt is a slice of pointers to int
type SliceOfPointerToInt []*int

// SliceOfPointerToNamedType is a slice of pointers to named types
type SliceOfPointerToNamedType []*IntType

// SliceOfNamedType is a slice of a named type
type SliceOfNamedType []IntType

// NamedSliceType is a slice of a named type.
type NamedSliceType SliceOfNamedType

// NamedNamedSliceType is another level of indirection from NamedSliceType
type NamedNamedSliceType NamedSliceType

// SliceOfNamedNamedSliceType is a slice of the named slice type.
type SliceOfNamedNamedSliceType []NamedNamedSliceType

// PointerToRemoteType is a pointer to a remote type.
type PointerToRemoteType *indirectsubpkg.IntType

// DefinedAsRemoteType is defined by another definition
type DefinedAsRemoteType indirectsubpkg.IntType

// DefinedAsRemoteSliceType is defined as a remote type
type DefinedAsRemoteSliceType indirectsubpkg.SliceOfNamedNamedSliceType

// DefinedAsPointerToRemoteSliceType is defined as a pointer to slice of named named slice type
type DefinedAsPointerToRemoteSliceType *indirectsubpkg.SliceOfNamedNamedSliceType

// DefinedAsSliceOfRemoteSliceType is defined as a slice of pointer to slice of named named slice type
type DefinedAsSliceOfRemoteSliceType []*indirectsubpkg.SliceOfNamedNamedSliceType
