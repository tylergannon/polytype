package basictypes

//go:generate go run ./gen

import (
	"github.com/tylergannon/polytype/internal/builder/testfixtures/enums/enumsremote"
)

// EnumType is an enum type from enumsremote
type EnumType string

const (
	// EnumVal1 is a value!!
	EnumVal1 EnumType = "val1"
	// EnumVal2 is also a value!!
	EnumVal2 EnumType = "val2"
	// EnumVal3 is truly a value!!
	EnumVal3 EnumType = "val3"
)

// EnumVal4 is the fourth value
const EnumVal4 EnumType = "val4"

// SliceOfEnumType is a slice of the enums.
type SliceOfEnumType []EnumType

// SliceOfRemoteEnumType is a slice of the remote enum type
type SliceOfRemoteEnumType []enumsremote.RemoteEnumType

// SliceOfPointerToRemoteEnum is a slice of pointers to the remote enum type
type SliceOfPointerToRemoteEnum []*enumsremote.RemoteEnumType

// EnumType declares itself as an enum; the generator emits its typed constants.
func (EnumType) enum() {}

// ZeroValueEnum has the empty string as a declared member, so JSON null must
// not be mistaken for it when decoding.
type ZeroValueEnum string

const (
	// ZeroValueEmpty is the zero value of the underlying string type.
	ZeroValueEmpty ZeroValueEnum = ""
	// ZeroValueSet is a non-zero member.
	ZeroValueSet ZeroValueEnum = "set"
)

// ZeroValueEnum declares itself as an enum.
func (ZeroValueEnum) enum() {}

// ZeroCodeEnum has zero as a declared member, so JSON null must not be
// mistaken for it when decoding.
type ZeroCodeEnum int

const (
	// ZeroCodeNone is the zero value of the underlying integer type.
	ZeroCodeNone ZeroCodeEnum = 0
	// ZeroCodeOne is a non-zero member.
	ZeroCodeOne ZeroCodeEnum = 1
)

// ZeroCodeEnum declares itself as an enum.
func (ZeroCodeEnum) enum() {}
