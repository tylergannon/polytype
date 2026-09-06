package devalue

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// A Reducer is a custom serializer, the Go form of devalue's `reducers`
// argument. Fn reports ok=false when it does not apply to the value, in which
// case the next reducer (and finally the built-in handling) is tried. A reducer
// that applies emits ["<Key>", i] where i indexes the replacement value.
//
// Reducers are held in a slice rather than a map because they are tried in
// order and the order is observable in the output.
type Reducer struct {
	Key string
	Fn  func(v any) (any, bool, error)
}

// Stringify serializes a value into devalue's flat-array format.
func Stringify(v any) (string, error) {
	return StringifyWith(v, nil)
}

// StringifyWith is Stringify with custom reducers, which run before the
// built-in type handling.
func StringifyWith(v any, reducers []Reducer) (string, error) {
	s := &stringifier{
		indexes:  make(map[any]int),
		reducers: reducers,
	}

	index, err := s.flatten(v)
	if err != nil {
		return "", err
	}
	if index < 0 {
		return strconv.Itoa(index), nil
	}

	return "[" + strings.Join(s.out, ",") + "]", nil
}

type stringifier struct {
	out      []string
	indexes  map[any]int
	reducers []Reducer
}

func (s *stringifier) set(index int, str string) {
	for len(s.out) <= index {
		s.out = append(s.out, "")
	}
	s.out[index] = str
}

func (s *stringifier) flatten(v any) (int, error) {
	switch v.(type) {
	case UndefinedValue:
		return sentinelUndefined, nil
	case HoleValue:
		return 0, errors.New("devalue: a hole can only appear inside an array")
	}

	if f, ok := asFloat(v); ok {
		switch {
		case math.IsNaN(f):
			return sentinelNaN, nil
		case math.IsInf(f, 1):
			return sentinelPosInf, nil
		case math.IsInf(f, -1):
			return sentinelNegInf, nil
		case f == 0 && math.Signbit(f):
			return sentinelNegZero, nil
		}
		v = f
	}

	key, dedupe := identityKey(v)
	if dedupe {
		if i, ok := s.indexes[key]; ok {
			return i, nil
		}
	}

	index := len(s.out)
	s.set(index, "")
	if dedupe {
		s.indexes[key] = index
	}

	for _, r := range s.reducers {
		reduced, ok, err := r.Fn(v)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		i, err := s.flatten(reduced)
		if err != nil {
			return 0, err
		}
		s.set(index, `["`+r.Key+`",`+strconv.Itoa(i)+`]`)
		return index, nil
	}

	str, err := s.serialize(v)
	if err != nil {
		return 0, err
	}
	s.set(index, str)

	return index, nil
}

func (s *stringifier) serialize(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return formatNumber(t), nil
	case string:
		return quoteString(t), nil
	case BigInt:
		return `["BigInt","` + string(t) + `"]`, nil
	case Date:
		return `["Date","` + isoString(t) + `"]`, nil
	case RegExp:
		if t.Flags == "" {
			return `["RegExp",` + quoteString(t.Source) + `]`, nil
		}
		return `["RegExp",` + quoteString(t.Source) + `,"` + t.Flags + `"]`, nil
	case ArrayBuffer:
		return `["ArrayBuffer","` + base64.StdEncoding.EncodeToString(t) + `"]`, nil
	case *Boxed:
		i, err := s.flatten(t.Value)
		if err != nil {
			return "", err
		}
		return `["Object",` + strconv.Itoa(i) + `]`, nil
	case []any:
		return s.array(t)
	case *Set:
		var b strings.Builder
		b.WriteString(`["Set"`)
		for _, item := range t.items {
			i, err := s.flatten(item)
			if err != nil {
				return "", err
			}
			b.WriteString(",")
			b.WriteString(strconv.Itoa(i))
		}
		b.WriteString("]")
		return b.String(), nil
	case *Map:
		var b strings.Builder
		b.WriteString(`["Map"`)
		for _, e := range t.entries {
			ki, err := s.flatten(e.Key)
			if err != nil {
				return "", err
			}
			vi, err := s.flatten(e.Value)
			if err != nil {
				return "", err
			}
			b.WriteString(",")
			b.WriteString(strconv.Itoa(ki))
			b.WriteString(",")
			b.WriteString(strconv.Itoa(vi))
		}
		b.WriteString("]")
		return b.String(), nil
	case *Object:
		return s.object(propertyOrder(t.keys), t.NullProto, func(k string) any {
			return t.values[k]
		})
	case map[string]any:
		// A Go map has no property order, so keys are sorted to keep the output
		// deterministic. Sorting rather than rejecting is deliberate: it is
		// what lets a caller hand a plain Go map to a *query* argument, whose
		// canonical form is sorted anyway, and still get the bytes the client
		// computed. The sort is by UTF-16 code unit so that a map and the
		// equivalent *Object sorted by JavaScript agree. A caller that owns
		// property order should pass an *Object rather than a map.
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		SortStringsUTF16(keys)
		return s.object(propertyOrder(keys), false, func(k string) any {
			return t[k]
		})
	}

	return "", fmt.Errorf("Cannot stringify arbitrary non-POJOs (%T)", v) //nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
}

func (s *stringifier) array(items []any) (string, error) {
	length := len(items)

	firstHole := -1
	for i, item := range items {
		if _, ok := item.(HoleValue); ok {
			firstHole = i
			break
		}
	}

	sparse := false
	if firstHole >= 0 {
		// devalue's heuristic: holes cost 3 characters each, while the sparse
		// encoding costs a header plus an index per populated element.
		population := 0
		for _, item := range items {
			if _, ok := item.(HoleValue); !ok {
				population++
			}
		}
		d := len(strconv.Itoa(length))
		holeCost := (length - population) * 3
		sparseCost := 4 + d + population*(d+1)
		sparse = holeCost > sparseCost
	}

	var b strings.Builder

	if sparse {
		b.WriteString("[")
		b.WriteString(strconv.Itoa(sentinelSparse))
		b.WriteString(",")
		b.WriteString(strconv.Itoa(length))
		for i, item := range items {
			if _, ok := item.(HoleValue); ok {
				continue
			}
			idx, err := s.flatten(item)
			if err != nil {
				return "", err
			}
			b.WriteString(",")
			b.WriteString(strconv.Itoa(i))
			b.WriteString(",")
			b.WriteString(strconv.Itoa(idx))
		}
		b.WriteString("]")
		return b.String(), nil
	}

	b.WriteString("[")
	for i, item := range items {
		if i > 0 {
			b.WriteString(",")
		}
		if _, ok := item.(HoleValue); ok {
			b.WriteString(strconv.Itoa(sentinelHole))
			continue
		}
		idx, err := s.flatten(item)
		if err != nil {
			return "", err
		}
		b.WriteString(strconv.Itoa(idx))
	}
	b.WriteString("]")

	return b.String(), nil
}

func (s *stringifier) object(keys []string, nullProto bool, get func(string) any) (string, error) {
	var b strings.Builder

	if nullProto {
		b.WriteString(`["null"`)
		for _, k := range keys {
			if k == protoKey {
				return "", errors.New("Cannot stringify objects with __proto__ keys") //nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
			}
			i, err := s.flatten(get(k))
			if err != nil {
				return "", err
			}
			b.WriteString(",")
			b.WriteString(quoteString(k))
			b.WriteString(",")
			b.WriteString(strconv.Itoa(i))
		}
		b.WriteString("]")
		return b.String(), nil
	}

	b.WriteString("{")
	for n, k := range keys {
		if k == protoKey {
			return "", errors.New("Cannot stringify objects with __proto__ keys") //nolint:staticcheck // ST1005: the message is devalue's own, reproduced verbatim.
		}
		if n > 0 {
			b.WriteString(",")
		}
		i, err := s.flatten(get(k))
		if err != nil {
			return "", err
		}
		b.WriteString(quoteString(k))
		b.WriteString(":")
		b.WriteString(strconv.Itoa(i))
	}
	b.WriteString("}")

	return b.String(), nil
}

const protoKey = "__proto__"
