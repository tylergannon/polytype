package devalue

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// The tests in this file port devalue@5.9.4's test/uneval-primitives.test.js
// and the sparse-array regressions 5.9.3 added to test/index.test.js. A
// repeated BigInt, or string of at least 128 UTF-16 code units, is hoisted
// when that is shorter, so that the output grows linearly with the input; a
// sparse array is rebuilt without reserving every slot.

// repeatedPrimitives are upstream's three primitive makers. Each returns a
// value of the given size and the JavaScript that evaluates to the same
// primitive, written independently of the serializer under test.
var repeatedPrimitives = []struct {
	name string
	make func(n int) (value any, js string)
}{
	{"string", func(n int) (any, string) {
		s := strings.Repeat("x", n)
		return s, `'x'.repeat(` + itoa(n) + `)`
	}},
	{"escaped string", func(n int) (any, string) {
		s := strings.Repeat("</script>\n\"\\\x00\u2028\u2029", n)
		return s, `'</script>\n"\\\0\u2028\u2029'.repeat(` + itoa(n) + `)`
	}},
	{"bigint", func(n int) (any, string) {
		return BigInt(strings.Repeat("9", n)), `BigInt('9'.repeat(` + itoa(n) + `))`
	}},
}

// evalWith evaluates expr with `want` bound to the primitive wantJS builds and
// with the Box class the replacer tests emit, and returns the value.
func evalWith(t *testing.T, expr, wantJS string) (*goja.Runtime, goja.Value) {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(`function Box(value) { this.value = value }; var want = ` + wantJS); err != nil {
		t.Fatal(err)
	}
	v, err := vm.RunString("(" + expr + ")")
	if err != nil {
		t.Fatalf("evaluating %.200s: %v", expr, err)
	}
	return vm, v
}

// jsonString is JSON.stringify of a string. Unlike quoteString it leaves `<`,
// U+2028 and U+2029 alone, and unlike encoding/json it escapes neither.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestUnevalRepeatedPrimitiveOutputGrowsLinearly(t *testing.T) {
	for _, kind := range repeatedPrimitives {
		t.Run(kind.name, func(t *testing.T) {
			previous := 0
			for _, n := range []int{2000, 4000} {
				primitive, wantJS := kind.make(n)
				// n references to one flat slot: a compact document whose
				// naive expression form is quadratic in n.
				var slot string
				switch p := primitive.(type) {
				case string:
					slot = jsonString(p)
				case BigInt:
					slot = `["BigInt","` + string(p) + `"]`
				}
				encoded := "[[" + strings.Repeat("1,", n-1) + "1]," + slot + "]"

				value, err := Parse(encoded, nil)
				if err != nil {
					t.Fatal(err)
				}
				serialized, err := Uneval(value)
				if err != nil {
					t.Fatal(err)
				}

				size := utf16Len(serialized)
				if limit := utf16Len(encoded) * 4; size >= limit {
					t.Errorf("n=%d: %d code units, want fewer than %d", n, size, limit)
				}
				if previous > 0 && float64(size) >= float64(previous)*2.1 {
					t.Errorf("n=%d: %d code units, more than 2.1 times the %d for half as many", n, size, previous)
				}
				previous = size
				if strings.Contains(serialized, "<") {
					t.Errorf("n=%d: output contains '<'", n)
				}

				vm, v := evalWith(t, serialized, wantJS)
				assertJS(t, vm, v, fmt.Sprintf(`$.length === %d && $.every(function (x) { return x === want })`, n))
			}
		})
	}
}

func TestUnevalPrimitiveSharedByDistinctBoxesStaysCompact(t *testing.T) {
	for _, kind := range repeatedPrimitives {
		t.Run(kind.name, func(t *testing.T) {
			primitive, wantJS := kind.make(2000)
			boxes := make([]any, 2000)
			for i := range boxes {
				boxes[i] = NewBoxed(primitive)
			}
			encoded, err := Stringify(boxes)
			if err != nil {
				t.Fatal(err)
			}
			value, err := Parse(encoded, nil)
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := Uneval(value)
			if err != nil {
				t.Fatal(err)
			}

			if size, limit := utf16Len(serialized), utf16Len(encoded)*4; size >= limit {
				t.Errorf("%d code units, want fewer than %d", size, limit)
			}
			vm, v := evalWith(t, serialized, wantJS)
			assertJS(t, vm, v, `$.length === 2000 && new Set($).size === 2000 && `+
				`$.every(function (box) { return typeof box === 'object' && box.valueOf() === want })`)
		})
	}
}

func TestUnevalPrimitivePreservesSharedAndDistinctBoxIdentities(t *testing.T) {
	for _, kind := range repeatedPrimitives {
		t.Run(kind.name, func(t *testing.T) {
			primitive, wantJS := kind.make(256)
			box := NewBoxed(primitive)
			serialized, err := Uneval([]any{box, box, primitive, primitive, NewBoxed(primitive)})
			if err != nil {
				t.Fatal(err)
			}
			vm, v := evalWith(t, serialized, wantJS)
			assertJS(t, vm, v, `$[0] === $[1] && $[0].valueOf() === want && $[2] === want && `+
				`$[3] === want && $[4].valueOf() === want && $[0] !== $[4]`)
		})
	}
}

func TestUnevalPrimitiveRoundTripsInSharedAndCyclicContainers(t *testing.T) {
	for _, kind := range repeatedPrimitives {
		t.Run(kind.name, func(t *testing.T) {
			primitive, wantJS := kind.make(256)
			array := []any{primitive}
			m := NewMap(primitive, primitive)
			set := NewSet(primitive)
			box := NewBoxed(primitive)
			value := NewNullProtoObject(
				"primitive", primitive,
				"array", array,
				"array_again", array,
				"map", m,
				"map_again", m,
				"set", set,
				"set_again", set,
				"box", box,
				"box_again", box,
				"sparse", sparse(1001, map[int]any{1000: primitive}),
			)
			value.Set("self", value)

			serialized, err := Uneval(value)
			if err != nil {
				t.Fatal(err)
			}
			vm, v := evalWith(t, serialized, wantJS)
			assertJS(t, vm, v, `Object.getPrototypeOf($) === null && $.self === $ && $.primitive === want && `+
				`$.array === $.array_again && $.array[0] === want && `+
				`$.map === $.map_again && $.map.get(want) === want && `+
				`$.set === $.set_again && $.set.has(want) && `+
				`$.box === $.box_again && $.box.valueOf() === want && `+
				`$.sparse.length === 1001 && Object.keys($.sparse).length === 1 && $.sparse[1000] === want`)
		})
	}
}

func TestUnevalKeepsInexpensiveRepetitionsInline(t *testing.T) {
	pending := make([]any, 1000)
	for i := range pending {
		pending[i] = "pending"
	}
	pendingJSON, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		value any
		js    string
	}{
		{"short strings", []any{"a string", "a string"}, `["a string","a string"]`},
		{"empty strings and a short BigInt", []any{"", "", BigInt("1"), BigInt("1")}, `["","",1n,1n]`},
		{"a thousand short strings", pending, string(pendingJSON)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Uneval(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.js {
				t.Errorf("\n got: %.200s\nwant: %.200s", got, tt.js)
			}
		})
	}
}

func TestUnevalBoundsExpansionAroundShortStringCutoff(t *testing.T) {
	for _, length := range []int{127, 128, 129} {
		for _, char := range []string{"x", "<"} {
			t.Run(fmt.Sprintf("%d %s", length, char), func(t *testing.T) {
				value := make([]any, 2000)
				for i := range value {
					value[i] = strings.Repeat(char, length)
				}
				serialized, err := Uneval(value)
				if err != nil {
					t.Fatal(err)
				}

				if strings.Contains(serialized, "<") {
					t.Error("output contains '<'")
				}
				size := utf16Len(serialized)
				if length < minCountedStringLength {
					if !strings.HasPrefix(serialized, "[") {
						t.Errorf("a short string was hoisted: %.60s", serialized)
					}
					if limit := len(value)*(6*length+3) + 1; size > limit {
						t.Errorf("%d code units, want at most %d", size, limit)
					}
				} else if limit := 6*length + 3*len(value); size >= limit {
					t.Errorf("%d code units, want fewer than %d", size, limit)
				}

				vm, v := evalWith(t, serialized, fmt.Sprintf(`'%s'.repeat(%d)`, char, length))
				assertJS(t, vm, v, `$.length === 2000 && $.every(function (x) { return x === want })`)
			})
		}
	}
}

func TestUnevalPreservesOtherPrimitivesAlongsideHoistedValues(t *testing.T) {
	text := strings.Repeat("x", 256)
	negZero := math.Copysign(0, -1)
	value := []any{
		text, text, 0, negZero, 0, negZero, math.NaN(), math.NaN(),
		math.Inf(1), math.Inf(-1), Undefined, nil, true, false,
	}
	serialized, err := Uneval(value)
	if err != nil {
		t.Fatal(err)
	}
	vm, v := evalWith(t, serialized, `'x'.repeat(256)`)
	assertJS(t, vm, v, `$.length === 14 && $[0] === want && $[1] === want && `+
		`Object.is($[2], 0) && Object.is($[3], -0) && Object.is($[4], 0) && Object.is($[5], -0) && `+
		`Object.is($[6], NaN) && Object.is($[7], NaN) && $[8] === Infinity && $[9] === -Infinity && `+
		`$[10] === undefined && 10 in $ && $[11] === null && $[12] === true && $[13] === false`)
}

func TestUnevalPrimitiveHoistingKeepsReplacerScopes(t *testing.T) {
	type box struct{ value any }
	text := strings.Repeat("x", 256)
	shared := &box{value: []any{text, text}}
	value := []any{shared, shared, text, text}

	var visited []any
	replacer := func(v any, uneval func(any) (string, error)) (string, bool, error) {
		visited = append(visited, v)
		if b, ok := v.(*box); ok {
			inner, err := uneval(b.value)
			return "new Box(" + inner + ")", true, err
		}
		return "", false, nil
	}
	serialized, err := UnevalWith(value, replacer)
	if err != nil {
		t.Fatal(err)
	}

	// The nested emitter has its own namespace, so it hoists its own copy.
	quoted := `"` + text + `"`
	want := `(function(a,b){return [a,a,b,b]}(new Box((function(a){return [a,a]}(` + quoted + `))),` + quoted + `))`
	if serialized != want {
		t.Errorf("\n got: %s\nwant: %s", serialized, want)
	}

	if len(visited) != 3 {
		t.Fatalf("replacer saw %d values, want the outer array, the box and its array", len(visited))
	}
	if outer, ok := visited[0].([]any); !ok || &outer[0] != &value[0] {
		t.Errorf("replacer saw %v first, want the outer array", visited[0])
	}
	if visited[1] != any(shared) {
		t.Errorf("replacer saw %v second, want the box", visited[1])
	}
	if inner, ok := visited[2].([]any); !ok || &inner[0] != &shared.value.([]any)[0] {
		t.Errorf("replacer saw %v third, want the box's array", visited[2])
	}

	vm, v := evalWith(t, serialized, `'x'.repeat(256)`)
	assertJS(t, vm, v, `$[0] === $[1] && $[0] instanceof Box && $[0].value.length === 2 && `+
		`$[0].value[0] === want && $[0].value[1] === want && $[2] === want && $[3] === want`)
}

// TestUnevalReconstructsBoxOfHoistedPrimitive pins the IIFE shape: a hoisted
// box whose primitive is hoisted too starts as a `{}` placeholder and is
// rebuilt in the body, where the primitive's parameter is in scope. The
// expected output was produced by devalue 5.9.4.
func TestUnevalReconstructsBoxOfHoistedPrimitive(t *testing.T) {
	text := strings.Repeat("x", 256)
	box := NewBoxed(text)
	got, err := Uneval([]any{box, box, text, text, NewBoxed(text)})
	if err != nil {
		t.Fatal(err)
	}
	want := `(function(a,b){b=Object(a);return [b,b,a,a,Object(a)]}("` + text + `",{}))`
	if got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestUnevalSupportsMoreHoistedPrimitivesThanParameterLimit(t *testing.T) {
	const n = 66000
	texts := make([]any, n)
	for i := range texts {
		digits := itoa(i)
		texts[i] = strings.Repeat("x", 128-len(digits)) + digits
	}
	copied := append([]any(nil), texts...)
	value := []any{texts, copied}

	serialized, err := Uneval(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(serialized, "(function(){var[") {
		t.Fatalf("expected the destructuring form, got %.60s...", serialized)
	}
	naive, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(serialized) >= len(naive) {
		t.Errorf("%d bytes, want fewer than the %d of the inline form", len(serialized), len(naive))
	}

	if testing.Short() {
		return
	}
	vm, v := evalWith(t, serialized, `null`)
	assertJS(t, vm, v, `$.length === 2 && $[0].length === 66000 && $[0].every(function (s, i) {`+
		`return s === 'x'.repeat(128 - String(i).length) + i && $[1][i] === s })`)
}

// TestUnevalSparseArraysAvoidEagerAllocation is devalue's "uneval evaluates
// sparse arrays without eager allocation", with a few arrays rather than
// 2500: a Go slice, unlike a JavaScript sparse array, holds every slot.
// Evaluating `Array(n)` would reserve all n; the allocator devalue emits
// instead does not, which the output shape is checked for.
func TestUnevalSparseArraysAvoidEagerAllocation(t *testing.T) {
	const length = 1_000_000
	for _, kind := range []string{"inline", "shared", "cyclic", "holes"} {
		t.Run(kind, func(t *testing.T) {
			arrays := make([]any, 2)
			for i := range arrays {
				a := sparse(length, map[int]any{0: 42})
				switch kind {
				case "holes":
					a[0] = Hole
				case "cyclic":
					a[1] = a
				default:
					a[1] = Undefined
				}
				arrays[i] = a
			}
			var value any = arrays
			if kind == "shared" {
				value = []any{arrays, append([]any(nil), arrays...)}
			}

			serialized, err := Uneval(value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(serialized, "Array(") {
				t.Fatalf("output preallocates: %.200s", serialized)
			}
			if !strings.Contains(serialized, "a.length=1000000;") {
				t.Fatalf("output does not use the sparse allocator: %.200s", serialized)
			}

			vm, v := evalWith(t, serialized, `null`)
			restored, shared := `$`, ``
			if kind == "shared" {
				restored, shared = `$[0]`, ` && $[0][i] === $[1][i]`
			}
			keys, elements := `['0','1','length']`, ` && a[0] === 42 && a[1] === undefined`
			switch kind {
			case "holes":
				keys, elements = `['length']`, ``
			case "cyclic":
				elements = ` && a[0] === 42 && a[1] === a`
			}
			assertJS(t, vm, v, `(function (r) { return r.length === 2 && [0, 1].every(function (i) {`+
				`var a = r[i]; return Array.isArray(a) && a.length === 1000000 && `+
				`Object.getOwnPropertyNames(a).join() === `+keys+`.join() && !(999999 in a)`+elements+shared+
				`}) })(`+restored+`)`)
		})
	}
}

// TestUnevalHoistedSparseArrayPreallocationBound pins when a hoisted array is
// preallocated with Array(n): only while its length is at most 32 plus twice
// its population. The expected output was produced by devalue 5.9.4.
func TestUnevalHoistedSparseArrayPreallocationBound(t *testing.T) {
	tests := []struct {
		length int
		js     string
	}{
		{34, `(function(a){a[0]=a;return a}(Array(34)))`},
		{35, `(function(a){a[0]=a;return a}((function(a){a[4294967294]=0;delete a[4294967294];a.length=35;return a}([]))))`},
	}
	for _, tt := range tests {
		t.Run(itoa(tt.length), func(t *testing.T) {
			a := sparse(tt.length, nil)
			a[0] = a
			got, err := Uneval(a)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.js {
				t.Errorf("\n got: %s\nwant: %s", got, tt.js)
			}
			vm, v := evalJS(t, got)
			assertJS(t, vm, v, `$.length === `+itoa(tt.length)+` && $[0] === $ && Object.keys($).length === 1`)
		})
	}
}
