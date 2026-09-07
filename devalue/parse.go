package devalue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	//nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
	errInvalidInput = errors.New("Invalid input")
	//nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
	errProto = errors.New("Cannot parse an object with a `__proto__` property")
)

// Parse parses devalue's flat format. revivers maps a custom type tag to a
// function that turns the already-parsed payload into a value; revivers take
// precedence over the built-in tags.
//
// Values come back as: nil for null, Undefined for undefined, bool, float64,
// string, []any (with Hole in empty slots), *Object, *Map, *Set, Date, BigInt,
// RegExp, ArrayBuffer and *Boxed.
func Parse(s string, revivers map[string]func(any) (any, error)) (any, error) {
	var top json.RawMessage
	if err := json.Unmarshal([]byte(s), &top); err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(top)
	if len(trimmed) == 0 {
		return nil, errInvalidInput
	}

	p := &parser{revivers: revivers}

	if trimmed[0] != '[' {
		// The whole payload is a bare sentinel, e.g. "-1" for undefined.
		n, err := strconv.Atoi(string(trimmed))
		if err != nil {
			return nil, errInvalidInput
		}
		return p.hydrate(n, true)
	}

	if err := json.Unmarshal(trimmed, &p.values); err != nil {
		return nil, errInvalidInput
	}
	if len(p.values) == 0 {
		return nil, errInvalidInput
	}
	p.grow()

	return p.hydrate(0, false)
}

type parser struct {
	values    []json.RawMessage
	hydrated  []any
	filled    []bool
	revivers  map[string]func(any) (any, error)
	hydrating map[int]bool
}

func (p *parser) grow() {
	for len(p.hydrated) < len(p.values) {
		p.hydrated = append(p.hydrated, nil)
		p.filled = append(p.filled, false)
	}
}

func (p *parser) store(index int, v any) {
	p.grow()
	p.hydrated[index] = v
	p.filled[index] = true
}

func (p *parser) hydrate(index int, standalone bool) (any, error) {
	switch index {
	case sentinelUndefined:
		return Undefined, nil
	case sentinelNaN:
		return math.NaN(), nil
	case sentinelPosInf:
		return math.Inf(1), nil
	case sentinelNegInf:
		return math.Inf(-1), nil
	case sentinelNegZero:
		return math.Copysign(0, -1), nil
	}

	if standalone {
		return nil, errInvalidInput
	}

	if index < 0 {
		// Mirrors JavaScript reading past the start of the array: the slot is
		// absent, which reads back as undefined.
		return Undefined, nil
	}

	if index < len(p.filled) && p.filled[index] {
		return p.hydrated[index], nil
	}
	if index >= len(p.values) {
		return nil, errInvalidInput
	}

	raw := p.values[index]

	switch firstByte(raw) {
	case '[':
		return p.hydrateArrayLike(index, raw)
	case '{':
		return p.hydrateObject(index, raw)
	default:
		v, err := decodePrimitive(raw)
		if err != nil {
			return nil, err
		}
		p.store(index, v)
		return v, nil
	}
}

// hydrateRef resolves a slot reference, which must be a number.
func (p *parser) hydrateRef(raw json.RawMessage) (any, error) {
	n, ok := asIndex(raw)
	if !ok {
		return nil, errInvalidInput
	}
	return p.hydrate(n, false)
}

func (p *parser) hydrateArrayLike(index int, raw json.RawMessage) (any, error) {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, errInvalidInput
	}

	if len(elems) > 0 && firstByte(elems[0]) == '"' {
		var tag string
		if err := json.Unmarshal(elems[0], &tag); err != nil {
			return nil, errInvalidInput
		}
		return p.hydrateTagged(index, tag, elems)
	}

	if len(elems) > 0 {
		if n, ok := asIndex(elems[0]); ok && n == sentinelSparse {
			return p.hydrateSparse(index, elems)
		}
	}

	return p.hydrateArray(index, elems)
}

func (p *parser) hydrateArray(index int, elems []json.RawMessage) (any, error) {
	array := make([]any, len(elems))
	for i := range array {
		array[i] = Hole
	}
	p.store(index, array)

	for i, elem := range elems {
		n, ok := asIndex(elem)
		if ok && n == sentinelHole {
			continue
		}
		v, err := p.hydrateRef(elem)
		if err != nil {
			return nil, err
		}
		array[i] = v
	}

	return array, nil
}

func (p *parser) hydrateSparse(index int, elems []json.RawMessage) (any, error) {
	if len(elems) < 2 {
		return nil, errInvalidInput
	}

	length, ok := asIndex(elems[1])
	if !ok || length < 0 || length > maxArrayLen {
		return nil, errInvalidInput
	}
	if length > maxParsedArrayLen {
		return nil, fmt.Errorf("devalue: sparse array of length %d exceeds the limit of %d", length, maxParsedArrayLen)
	}

	array := make([]any, length)
	for i := range array {
		array[i] = Hole
	}
	p.store(index, array)

	for i := 2; i+1 < len(elems); i += 2 {
		idx, ok := asIndex(elems[i])
		if !ok || idx < 0 || idx >= length {
			return nil, errInvalidInput
		}
		v, err := p.hydrateRef(elems[i+1])
		if err != nil {
			return nil, err
		}
		array[idx] = v
	}

	return array, nil
}

func (p *parser) hydrateTagged(index int, tag string, elems []json.RawMessage) (any, error) {
	if fn, ok := p.revivers[tag]; ok {
		return p.revive(index, fn, elems)
	}

	switch tag {
	case "Date":
		var iso string
		if len(elems) < 2 || json.Unmarshal(elems[1], &iso) != nil {
			return nil, errInvalidInput
		}
		if iso == "" {
			// JavaScript's Invalid Date; Go has no such value, so use the
			// zero time.
			p.store(index, Date(time.Time{}))
			return p.hydrated[index], nil
		}
		t, err := time.Parse(time.RFC3339Nano, iso)
		if err != nil {
			return nil, fmt.Errorf("devalue: invalid Date %q", iso)
		}
		p.store(index, Date(t))
		return p.hydrated[index], nil

	case "Set":
		set := &Set{}
		p.store(index, set)
		for i := 1; i < len(elems); i++ {
			v, err := p.hydrateRef(elems[i])
			if err != nil {
				return nil, err
			}
			set.Add(v)
		}
		return set, nil

	case "Map":
		m := &Map{}
		p.store(index, m)
		for i := 1; i+1 < len(elems); i += 2 {
			k, err := p.hydrateRef(elems[i])
			if err != nil {
				return nil, err
			}
			v, err := p.hydrateRef(elems[i+1])
			if err != nil {
				return nil, err
			}
			m.Set(k, v)
		}
		return m, nil

	case "RegExp":
		re := RegExp{}
		if len(elems) < 2 || json.Unmarshal(elems[1], &re.Source) != nil {
			return nil, errInvalidInput
		}
		if len(elems) > 2 {
			if err := json.Unmarshal(elems[2], &re.Flags); err != nil {
				return nil, errInvalidInput
			}
		}
		p.store(index, re)
		return re, nil

	case "BigInt":
		var digits string
		if len(elems) < 2 || json.Unmarshal(elems[1], &digits) != nil {
			return nil, errInvalidInput
		}
		p.store(index, BigInt(digits))
		return p.hydrated[index], nil

	case "ArrayBuffer":
		var b64 string
		if len(elems) < 2 || json.Unmarshal(elems[1], &b64) != nil {
			//nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
			return nil, errors.New("Invalid ArrayBuffer encoding")
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			//nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
			return nil, errors.New("Invalid ArrayBuffer encoding")
		}
		buf := ArrayBuffer(data)
		p.store(index, buf)
		return buf, nil

	case "Object":
		if len(elems) < 2 {
			return nil, errInvalidInput
		}
		if wrapped, ok := asIndex(elems[1]); ok && wrapped >= 0 {
			// Guard against a boxed primitive pointing at a container, which
			// would otherwise recurse forever.
			if wrapped >= len(p.values) {
				return nil, errInvalidInput
			}
			if c := firstByte(p.values[wrapped]); c == '{' || c == 'n' {
				return nil, errInvalidInput
			} else if c == '[' && !p.isBigIntSlot(wrapped) {
				return nil, errInvalidInput
			}
		} else if !ok {
			return nil, errInvalidInput
		}
		boxed := &Boxed{}
		p.store(index, boxed)
		v, err := p.hydrateRef(elems[1])
		if err != nil {
			return nil, err
		}
		boxed.Value = v
		return boxed, nil

	case "null":
		obj := &Object{NullProto: true}
		p.store(index, obj)
		for i := 1; i+1 < len(elems); i += 2 {
			var key string
			if err := json.Unmarshal(elems[i], &key); err != nil {
				return nil, errInvalidInput
			}
			if key == protoKey {
				return nil, errProto
			}
			v, err := p.hydrateRef(elems[i+1])
			if err != nil {
				return nil, err
			}
			obj.Set(key, v)
		}
		return obj, nil
	}

	return nil, fmt.Errorf("Unknown type %s", tag) //nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
}

func (p *parser) isBigIntSlot(index int) bool {
	var elems []json.RawMessage
	if json.Unmarshal(p.values[index], &elems) != nil || len(elems) == 0 {
		return false
	}
	var tag string
	return json.Unmarshal(elems[0], &tag) == nil && tag == "BigInt"
}

func (p *parser) revive(index int, fn func(any) (any, error), elems []json.RawMessage) (any, error) {
	if len(elems) < 2 {
		out, err := fn(Undefined)
		if err != nil {
			return nil, err
		}
		p.store(index, out)
		return out, nil
	}

	i, ok := asIndex(elems[1])
	if !ok {
		// The payload was written inline by a built-in reducer rather than as
		// a reference, so give it a slot of its own.
		p.values = append(p.values, elems[1])
		p.grow()
		i = len(p.values) - 1
	}

	if i >= 0 && i < len(p.filled) && p.filled[i] {
		out, err := fn(p.hydrated[i])
		if err != nil {
			return nil, err
		}
		p.store(index, out)
		return out, nil
	}

	if p.hydrating == nil {
		p.hydrating = make(map[int]bool)
	}
	if p.hydrating[i] {
		return nil, errors.New("Invalid circular reference") //nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
	}
	p.hydrating[i] = true
	defer delete(p.hydrating, i)

	v, err := p.hydrate(i, false)
	if err != nil {
		return nil, err
	}
	out, err := fn(v)
	if err != nil {
		return nil, err
	}
	p.store(index, out)

	return out, nil
}

func (p *parser) hydrateObject(index int, raw json.RawMessage) (any, error) {
	entries, err := decodeObjectEntries(raw)
	if err != nil {
		return nil, err
	}

	obj := &Object{}
	p.store(index, obj)

	byKey := make(map[string]json.RawMessage, len(entries))
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, seen := byKey[e.key]; !seen {
			keys = append(keys, e.key)
		}
		byKey[e.key] = e.value
	}

	for _, key := range propertyOrder(keys) {
		if key == protoKey {
			return nil, errProto
		}
		v, err := p.hydrateRef(byKey[key])
		if err != nil {
			return nil, err
		}
		obj.Set(key, v)
	}

	return obj, nil
}

type rawEntry struct {
	key   string
	value json.RawMessage
}

func decodeObjectEntries(raw json.RawMessage) ([]rawEntry, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // consume '{'
		return nil, errInvalidInput
	}

	var entries []rawEntry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errInvalidInput
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errInvalidInput
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, errInvalidInput
		}
		entries = append(entries, rawEntry{key: key, value: value})
	}

	return entries, nil
}

func decodePrimitive(raw json.RawMessage) (any, error) {
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case trimmed == "null":
		return nil, nil
	case trimmed == "true":
		return true, nil
	case trimmed == "false":
		return false, nil
	case strings.HasPrefix(trimmed, `"`):
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, errInvalidInput
		}
		return s, nil
	}

	f, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return nil, errInvalidInput
	}
	return f, nil
}

func firstByte(raw []byte) byte {
	for _, c := range raw {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return c
		}
	}
	return 0
}

func asIndex(raw json.RawMessage) (int, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, false
	}
	if math.IsInf(f, 0) || math.IsNaN(f) || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}
