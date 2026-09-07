// Package devalue implements the `devalue` wire format for structured values.
//
// It is a port of the JavaScript `devalue` package's flat "stringify"/"parse"
// pair (the JSON-array form, not `uneval`), scoped to the value shapes that
// travel over that format in practice: plain and null-prototype objects,
// arrays with holes, strings, numbers, booleans, null, undefined, Date, Map,
// Set, BigInt, RegExp, ArrayBuffer and boxed primitives. Typed arrays,
// DataView, URL, URLSearchParams and Temporal values are deliberately not
// implemented; a payload containing one parses to an "Unknown type" error.
package devalue

import (
	"bytes"
	"encoding/json"
	"time"
)

// Undefined is the JavaScript value `undefined`. It is what Parse returns for
// the payload "-1", and what Stringify writes as the bare token "-1".
var Undefined = UndefinedValue{}

// UndefinedValue is the type of Undefined.
type UndefinedValue struct{}

func (UndefinedValue) String() string { return "undefined" }

// Hole is an empty slot in a sparse array. It appears in the []any produced by
// Parse wherever the array had no element, and may be used in a []any passed to
// Stringify to produce holes.
var Hole = HoleValue{}

// HoleValue is the type of Hole.
type HoleValue struct{}

func (HoleValue) String() string { return "<hole>" }

// Object is a JavaScript plain object with an explicit property order.
//
// Property order is part of the serialized bytes, so Stringify accepts both
// *Object (order preserved) and map[string]any (keys sorted, since a Go map has
// no order). Parse always produces *Object.
type Object struct {
	// NullProto reports whether this is an `Object.create(null)` object,
	// which devalue tags as ["null", key, value, ...].
	NullProto bool

	keys   []string
	values map[string]any
}

// NewObject builds an object from alternating key/value pairs. It panics if the
// arguments are not pairs of (string, any).
func NewObject(kv ...any) *Object {
	o := &Object{}
	o.setPairs(kv)
	return o
}

// NewNullProtoObject is NewObject for a null-prototype object.
func NewNullProtoObject(kv ...any) *Object {
	o := &Object{NullProto: true}
	o.setPairs(kv)
	return o
}

func (o *Object) setPairs(kv []any) {
	if len(kv)%2 != 0 {
		panic("devalue: NewObject requires alternating key/value arguments")
	}
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			panic("devalue: NewObject keys must be strings")
		}
		o.Set(key, kv[i+1])
	}
}

// Set assigns a property, appending it if new and keeping its original position
// if it already exists.
func (o *Object) Set(key string, value any) {
	if o.values == nil {
		o.values = make(map[string]any)
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Get returns the named property.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.values[key]
	return v, ok
}

// Keys returns the property names in insertion order.
func (o *Object) Keys() []string {
	out := make([]string, len(o.keys))
	copy(out, o.keys)
	return out
}

// Len returns the number of properties.
func (o *Object) Len() int { return len(o.keys) }

// MapEntry is one key/value pair of a Map.
type MapEntry struct {
	Key   any
	Value any
}

// Map is a JavaScript Map: ordered entries, keys compared by identity the way
// SameValueZero compares them (primitives by value, containers by reference).
type Map struct {
	entries []MapEntry
}

// NewMap builds a Map from alternating key/value arguments.
func NewMap(kv ...any) *Map {
	if len(kv)%2 != 0 {
		panic("devalue: NewMap requires alternating key/value arguments")
	}
	m := &Map{}
	for i := 0; i < len(kv); i += 2 {
		m.Set(kv[i], kv[i+1])
	}
	return m
}

// Set adds or replaces an entry.
func (m *Map) Set(key, value any) {
	if i := m.indexOf(key); i >= 0 {
		m.entries[i].Value = value
		return
	}
	m.entries = append(m.entries, MapEntry{Key: key, Value: value})
}

// Get returns the value stored under key.
func (m *Map) Get(key any) (any, bool) {
	if i := m.indexOf(key); i >= 0 {
		return m.entries[i].Value, true
	}
	return nil, false
}

func (m *Map) indexOf(key any) int {
	k, ok := identityKey(key)
	if !ok {
		return -1
	}
	for i, e := range m.entries {
		if ek, ok := identityKey(e.Key); ok && ek == k {
			return i
		}
	}
	return -1
}

// Entries returns the entries in insertion order.
func (m *Map) Entries() []MapEntry {
	out := make([]MapEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Len returns the number of entries.
func (m *Map) Len() int { return len(m.entries) }

// Set is a JavaScript Set: ordered items, deduplicated by identity.
type Set struct {
	items []any
}

// NewSet builds a Set from its items.
func NewSet(items ...any) *Set {
	s := &Set{}
	for _, item := range items {
		s.Add(item)
	}
	return s
}

// Add appends an item unless an identical one is already present.
func (s *Set) Add(v any) {
	if s.Has(v) {
		return
	}
	s.items = append(s.items, v)
}

// Has reports whether an identical item is present.
func (s *Set) Has(v any) bool {
	k, ok := identityKey(v)
	if !ok {
		return false
	}
	for _, item := range s.items {
		if ik, ok := identityKey(item); ok && ik == k {
			return true
		}
	}
	return false
}

// Items returns the items in insertion order.
func (s *Set) Items() []any {
	out := make([]any, len(s.items))
	copy(out, s.items)
	return out
}

// Len returns the number of items.
func (s *Set) Len() int { return len(s.items) }

// Date is a JavaScript Date. It serializes as an ISO 8601 string with
// millisecond precision in UTC.
type Date time.Time

// Time returns the underlying time.
func (d Date) Time() time.Time { return time.Time(d) }

// BigInt is a JavaScript BigInt, held as its decimal digits.
type BigInt string

// RegExp is a JavaScript regular expression. Kit rejects these as remote
// function arguments, but the wire format can carry them.
type RegExp struct {
	Source string
	Flags  string
}

// ArrayBuffer is a JavaScript ArrayBuffer, serialized as base64.
type ArrayBuffer []byte

// Boxed is a boxed primitive — `Object(42)`, `new String("x")` — serialized as
// ["Object", i]. Always use *Boxed so that repeated references share identity.
type Boxed struct {
	Value any
}

// NewBoxed boxes a primitive.
func NewBoxed(v any) *Boxed { return &Boxed{Value: v} }

// MarshalJSON renders the object the way JSON.stringify would, so a parsed
// devalue tree can be round-tripped through encoding/json into a typed Go
// value. Property order is preserved, and an `undefined` property is omitted
// exactly as JSON.stringify omits it.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, key := range o.keys {
		value := o.values[key]
		if value == Undefined {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		b.Write(name)
		b.WriteByte(':')
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		b.Write(raw)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// MarshalJSON renders `undefined` as null, which is what JSON.stringify does
// with it inside an array.
func (UndefinedValue) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// MarshalJSON renders an array hole as null, as JSON.stringify does.
func (HoleValue) MarshalJSON() ([]byte, error) { return []byte("null"), nil }
