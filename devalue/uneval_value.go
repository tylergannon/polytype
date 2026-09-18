package devalue

import (
	"encoding/binary"
	"math"
	"reflect"
)

// URL is a JavaScript URL, held as the serialized href returned by
// URL.prototype.toString. The caller owns WHATWG URL normalization.
type URL string

// URLSearchParams is a JavaScript URLSearchParams, held as its serialized
// query string — what `params.toString()` returns.
type URLSearchParams string

// TemporalKind names one of the Temporal types devalue serializes. It is the
// JavaScript constructor path, which is also what the emitted expression uses.
type TemporalKind string

// The Temporal types devalue serializes.
const (
	TemporalDuration       TemporalKind = "Temporal.Duration"
	TemporalInstant        TemporalKind = "Temporal.Instant"
	TemporalPlainDate      TemporalKind = "Temporal.PlainDate"
	TemporalPlainTime      TemporalKind = "Temporal.PlainTime"
	TemporalPlainDateTime  TemporalKind = "Temporal.PlainDateTime"
	TemporalPlainMonthDay  TemporalKind = "Temporal.PlainMonthDay"
	TemporalPlainYearMonth TemporalKind = "Temporal.PlainYearMonth"
	TemporalZonedDateTime  TemporalKind = "Temporal.ZonedDateTime"
)

// Temporal is a Temporal value, held as its kind and its `toString()` form —
// the round-trip form `Kind.from(...)` accepts.
type Temporal struct {
	Kind  TemporalKind
	Value string
}

// TypedArrayKind names a JavaScript typed array constructor.
type TypedArrayKind string

// The typed array kinds devalue serializes. Float16Array is deliberately
// absent: Go has no half-precision float and devalue's own test suite never
// exercises it.
const (
	Int8Array         TypedArrayKind = "Int8Array"
	Uint8Array        TypedArrayKind = "Uint8Array"
	Uint8ClampedArray TypedArrayKind = "Uint8ClampedArray"
	Int16Array        TypedArrayKind = "Int16Array"
	Uint16Array       TypedArrayKind = "Uint16Array"
	Int32Array        TypedArrayKind = "Int32Array"
	Uint32Array       TypedArrayKind = "Uint32Array"
	Float32Array      TypedArrayKind = "Float32Array"
	Float64Array      TypedArrayKind = "Float64Array"
	BigInt64Array     TypedArrayKind = "BigInt64Array"
	BigUint64Array    TypedArrayKind = "BigUint64Array"
)

// BytesPerElement is the kind's BYTES_PER_ELEMENT, or 0 if the kind is unknown.
func (k TypedArrayKind) BytesPerElement() int {
	switch k {
	case Int8Array, Uint8Array, Uint8ClampedArray:
		return 1
	case Int16Array, Uint16Array:
		return 2
	case Int32Array, Uint32Array, Float32Array:
		return 4
	case Float64Array, BigInt64Array, BigUint64Array:
		return 8
	}
	return 0
}

// TypedArray is a JavaScript typed array: a view of a byte range of an
// [ArrayBuffer].
//
// Buffer is always the *whole* underlying buffer, because that is what devalue
// serializes — the view's own extent is emitted as a trailing `.subarray(a,b)`.
// Two views over the same Buffer value share a buffer in the emitted output,
// which is the property `new Uint16Array(uint8.buffer)` has in JavaScript.
//
// Use a *TypedArray: identity, and therefore reference sharing, is the
// pointer's.
type TypedArray struct {
	Kind       TypedArrayKind
	Buffer     ArrayBuffer
	ByteOffset int
	ByteLength int
}

// NewTypedArray views the whole of buf as kind.
func NewTypedArray(kind TypedArrayKind, buf ArrayBuffer) *TypedArray {
	return &TypedArray{Kind: kind, Buffer: buf, ByteLength: len(buf)}
}

// Subarray returns the [start, end) element range of t, as
// `TypedArray#subarray` does. It shares t's buffer.
func (t *TypedArray) Subarray(start, end int) *TypedArray {
	n := t.Kind.BytesPerElement()
	return &TypedArray{
		Kind:       t.Kind,
		Buffer:     t.Buffer,
		ByteOffset: t.ByteOffset + start*n,
		ByteLength: (end - start) * n,
	}
}

// Len returns the number of elements in the view.
func (t *TypedArray) Len() int {
	n := t.Kind.BytesPerElement()
	if n == 0 {
		return 0
	}
	return t.ByteLength / n
}

// DataView is a JavaScript DataView over an [ArrayBuffer]. As with
// [TypedArray], Buffer is the whole buffer and the view's extent is separate.
type DataView struct {
	Buffer     ArrayBuffer
	ByteOffset int
	ByteLength int
}

// NewDataView views the whole of buf.
func NewDataView(buf ArrayBuffer) *DataView {
	return &DataView{Buffer: buf, ByteLength: len(buf)}
}

// NewDataViewRange views byteLength bytes of buf starting at byteOffset.
func NewDataViewRange(buf ArrayBuffer, byteOffset, byteLength int) *DataView {
	return &DataView{Buffer: buf, ByteOffset: byteOffset, ByteLength: byteLength}
}

// Constructors that build a buffer from elements, mirroring
// `new Uint16Array([...])`. Elements are laid out little-endian, which is the
// byte order of every platform a browser runs on.

func Int8ArrayOf(v ...int8) *TypedArray {
	b := make([]byte, len(v))
	for i, x := range v {
		b[i] = byte(x)
	}
	return NewTypedArray(Int8Array, b)
}

func Uint8ArrayOf(v ...uint8) *TypedArray {
	return NewTypedArray(Uint8Array, append(ArrayBuffer(nil), v...))
}

func Uint8ClampedArrayOf(v ...uint8) *TypedArray {
	return NewTypedArray(Uint8ClampedArray, append(ArrayBuffer(nil), v...))
}

func Int16ArrayOf(v ...int16) *TypedArray {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(x))
	}
	return NewTypedArray(Int16Array, b)
}

func Uint16ArrayOf(v ...uint16) *TypedArray {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], x)
	}
	return NewTypedArray(Uint16Array, b)
}

func Int32ArrayOf(v ...int32) *TypedArray {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], uint32(x))
	}
	return NewTypedArray(Int32Array, b)
}

func Uint32ArrayOf(v ...uint32) *TypedArray {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	return NewTypedArray(Uint32Array, b)
}

func Float32ArrayOf(v ...float32) *TypedArray {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return NewTypedArray(Float32Array, b)
}

func Float64ArrayOf(v ...float64) *TypedArray {
	b := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(x))
	}
	return NewTypedArray(Float64Array, b)
}

func BigInt64ArrayOf(v ...int64) *TypedArray {
	b := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[8*i:], uint64(x))
	}
	return NewTypedArray(BigInt64Array, b)
}

func BigUint64ArrayOf(v ...uint64) *TypedArray {
	b := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[8*i:], x)
	}
	return NewTypedArray(BigUint64Array, b)
}

// isPrimitive reports whether v is a JavaScript primitive, which is devalue's
// `is_primitive`: null, undefined, booleans, numbers, strings and bigints. A
// primitive is never hoisted into the IIFE and never reaches the replacer.
func isPrimitive(v any) bool {
	switch v.(type) {
	case nil, bool, string, UndefinedValue, BigInt:
		return true
	}
	_, ok := asFloat(v)
	return ok
}

func isHole(v any) bool {
	_, ok := v.(HoleValue)
	return ok
}

// referenceDateKey and the other value keys below stand in for reference identity for
// the types this package models as Go values rather than pointers. Two Go
// values that compare equal serialize identically, so treating them as one
// object is safe for hydration; the only visible difference from JavaScript is
// that two distinct-but-equal Dates share a reference in the output.
type referenceDateKey int64

// refKey returns a comparable key standing in for v's JavaScript object
// identity, and whether v participates in reference sharing at all.
//
// An empty array, buffer or map gets dedupe=false: Go cannot distinguish two
// empty slices (they may share the zero-size allocation), and collapsing
// `{a:[],b:[]}` into one shared array would be a real aliasing bug in the
// hydrated client. Such a value has no children and cannot take part in a
// cycle, so nothing is lost by not tracking it.
func refKey(v any) (any, bool) {
	rv := reflect.ValueOf(v)
	if rv.IsValid() {
		switch rv.Kind() {
		case reflect.Pointer, reflect.UnsafePointer, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice:
			if rv.IsNil() {
				return nil, false
			}
		}
	}

	switch t := v.(type) {
	case *Object:
		return t, true
	case *Map:
		return t, true
	case *Set:
		return t, true
	case *Boxed:
		return t, true
	case *TypedArray:
		return t, true
	case *DataView:
		return t, true
	case Date:
		return referenceDateKey(t.Time().UnixMilli()), true
	case RegExp:
		return t, true
	case URL:
		return t, true
	case URLSearchParams:
		return t, true
	case Temporal:
		return t, true
	case []any:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{"array", reflect.ValueOf(t).Pointer(), len(t)}, true
	case ArrayBuffer:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{"buffer", reflect.ValueOf(t).Pointer(), len(t)}, true
	case map[string]any:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{"map", reflect.ValueOf(t).Pointer(), -1}, true
	}

	// Anything else is a type this package does not model, which is still a
	// JavaScript object as far as the replacer is concerned: it has to be
	// counted and offered to the replacer before it can be rejected. Pointers,
	// maps, slices and channels key on identity; any other comparable value keys
	// on itself, which is how this package already treats Date and RegExp.
	switch rv.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Chan:
		// An interface holding a pointer compares by type and address.
		return v, true
	case reflect.Func:
		return nil, false
	case reflect.Map, reflect.Slice:
		if rv.Len() == 0 {
			return nil, false
		}
		return ptrKey{rv.Type().String(), rv.Pointer(), rv.Len()}, true
	}
	if rv.IsValid() && rv.Comparable() {
		return v, true
	}
	return nil, false
}
