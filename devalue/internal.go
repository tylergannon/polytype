package devalue

import (
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sentinels written bare at the root and in place of array elements.
const (
	sentinelUndefined = -1
	sentinelHole      = -2
	sentinelNaN       = -3
	sentinelPosInf    = -4
	sentinelNegInf    = -5
	sentinelNegZero   = -6
	sentinelSparse    = -7
)

// maxArrayLen mirrors the JavaScript limit on Array#length.
const maxArrayLen = 1<<32 - 1

// maxArrayIndex is the largest valid array index.
const maxArrayIndex = maxArrayLen - 1

// maxParsedArrayLen bounds how large an array Parse will allocate. JavaScript
// permits lengths up to maxArrayLen, but a Go []any of that size is 64GB, so a
// hostile payload declaring a huge sparse length is refused instead.
const maxParsedArrayLen = 1 << 21

type nullKey struct{}

type ptrKey struct {
	p uintptr
	n int
}

type dateKey string

// identityKey returns a comparable key standing in for JavaScript's
// SameValueZero identity, and whether the value participates in deduplication
// at all. Primitives dedupe by value; containers dedupe by reference.
func identityKey(v any) (any, bool) {
	switch t := v.(type) {
	case nil:
		return nullKey{}, true
	case bool:
		return t, true
	case string:
		return t, true
	case BigInt:
		return t, true
	case RegExp:
		return t, true
	case Date:
		return dateKey(isoString(t)), true
	case *Object:
		return t, true
	case *Map:
		return t, true
	case *Set:
		return t, true
	case *Boxed:
		return t, true
	case []any:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{reflect.ValueOf(t).Pointer(), len(t)}, true
	case ArrayBuffer:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{reflect.ValueOf(t).Pointer(), len(t)}, true
	case map[string]any:
		if len(t) == 0 {
			return nil, false
		}
		return ptrKey{reflect.ValueOf(t).Pointer(), -1}, true
	}

	if f, ok := asFloat(v); ok {
		return f, true
	}

	return nil, false
}

// asFloat converts any Go number to the float64 JavaScript would hold.
func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	}
	return 0, false
}

// isoString renders a Date the way Date#toISOString does.
func isoString(d Date) string {
	return time.Time(d).UTC().Format("2006-01-02T15:04:05.000Z")
}

// escapes maps the characters devalue's stringify_string replaces. Note that
// `<` becomes \\u003C so that output is safe to inline in a <script> element.
var escapes = map[rune]string{
	'"':      "\\\"",
	'<':      "\\u003C",
	'\\':     "\\\\",
	'\n':     "\\n",
	'\r':     "\\r",
	'\t':     "\\t",
	'\b':     "\\b",
	'\f':     "\\f",
	'\u2028': "\\u2028",
	'\u2029': "\\u2029",
}

// writeString escapes a string the way devalue's stringify_string does.
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		if esc, ok := escapes[r]; ok {
			b.WriteString(esc)
			continue
		}
		if r < ' ' {
			const hex = "0123456789abcdef"
			b.WriteString("\\u00")
			b.WriteByte(hex[(r>>4)&0xf])
			b.WriteByte(hex[r&0xf])
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
}

func quoteString(s string) string {
	var b strings.Builder
	writeString(&b, s)
	return b.String()
}

// formatNumber implements ECMAScript's Number::toString for base 10, which
// differs from every Go format verb around the exponent thresholds (JS switches
// to exponential notation below 1e-6 and at or above 1e21).
func formatNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}

	neg := f < 0
	if neg {
		f = -f
	}

	// Shortest round-tripping decimal, as digits and a decimal exponent.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, exponent, _ := strings.Cut(e, "e")
	exp, err := strconv.Atoi(exponent)
	if err != nil {
		return e
	}

	digits := strings.Replace(mantissa, ".", "", 1)
	k := len(digits)
	n := exp + 1 // position of the decimal point

	var s string
	switch {
	case k <= n && n <= 21:
		s = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		s = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		s = "0." + strings.Repeat("0", -n) + digits
	default:
		m := digits
		if k > 1 {
			m = digits[:1] + "." + digits[1:]
		}
		if n-1 >= 0 {
			s = m + "e+" + strconv.Itoa(n-1)
		} else {
			s = m + "e-" + strconv.Itoa(1-n)
		}
	}

	if neg {
		return "-" + s
	}
	return s
}

// isArrayIndexString reports whether a property name is a canonical array index,
// which JavaScript hoists ahead of ordinary string keys.
func isArrayIndexString(s string) bool {
	if s == "" || len(s) > 10 {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n <= maxArrayIndex
}

// propertyOrder returns keys in JavaScript property order: canonical array
// indices ascending, then the remaining keys in insertion order.
func propertyOrder(keys []string) []string {
	var indices, rest []string
	for _, k := range keys {
		if isArrayIndexString(k) {
			indices = append(indices, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(indices) == 0 {
		return rest
	}
	sort.Slice(indices, func(i, j int) bool {
		a, _ := strconv.ParseUint(indices[i], 10, 64)
		b, _ := strconv.ParseUint(indices[j], 10, 64)
		return a < b
	})
	return append(indices, rest...)
}
