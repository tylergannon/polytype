package devalue

import (
	"math"
	"strings"
	"testing"
)

// bs is a single backslash, used to build expected output without spelling
// escape sequences inside raw string literals.
const bs = "\\"

// fixtures are ported from devalue's own test suite (test/index.test.js), the
// `json` field of each fixture that is within scope here.
type fixture struct {
	name  string
	value any
	json  string
}

func fixtures() []fixture {
	cyclicalArray := make([]any, 1)
	cyclicalArray[0] = cyclicalArray

	cyclicalMap := NewMap()
	cyclicalMap.Set("self", cyclicalMap)

	cyclicalSet := NewSet()
	cyclicalSet.Add(cyclicalSet)
	cyclicalSet.Add(42.0)

	cyclicalObject := NewObject()
	cyclicalObject.Set("self", cyclicalObject)

	cyclicalNullProto := NewNullProtoObject()
	cyclicalNullProto.Set("self", cyclicalNullProto)

	first, second := NewObject(), NewObject()
	first.Set("second", second)
	second.Set("first", first)

	boxedNumber := NewBoxed(42.0)
	boxedBigInt := NewBoxed(BigInt("1"))
	boxedNaN := NewBoxed(math.NaN())
	emptyObject := NewObject()
	emptyMap := NewMap()
	emptySet := NewSet()
	regexp := RegExp{Source: "regexp"}
	date := Date(epochMillis(1e12))

	verySparse := holes(1000001)
	verySparse[1000000] = "x"

	multiSparse := holes(21)
	multiSparse[10] = "a"
	multiSparse[20] = "b"

	return []fixture{
		// primitives
		{"positive integer", 42, "[42]"},
		{"negative integer", -5, "[-5]"},
		{"uint8", uint8(42), "[42]"},
		{"positive decimal", 0.1, "[0.1]"},
		{"negative decimal", -0.1, "[-0.1]"},
		{"NaN", math.NaN(), "-3"},
		{"+Infinity", math.Inf(1), "-4"},
		{"-Infinity", math.Inf(-1), "-5"},
		{"zero", 0, "[0]"},
		{"negative zero", math.Copysign(0, -1), "-6"},
		{"string", "woo!!!", `["woo!!!"]`},
		{"boolean", true, "[true]"},
		{"bigint", BigInt("1"), `[["BigInt","1"]]`},
		{"undefined", Undefined, "-1"},
		{"null", nil, "[null]"},

		// basics
		{"RegExp", RegExp{Source: "regexp", Flags: "gim"}, `[["RegExp","regexp","gim"]]`},
		{"Date", date, `[["Date","2001-09-09T01:46:40.000Z"]]`},
		{"Array", []any{"a", "b", "c"}, `[[1,2,3],"a","b","c"]`},
		{"negative zero after zero", []any{0, math.Copysign(0, -1)}, "[[1,-6],0]"},
		{"empty Array", []any{}, "[[]]"},
		{"sparse Array", []any{Hole, "b", Hole}, `[[-2,1,-2],"b"]`},
		{"very sparse Array", verySparse, `[[-7,1000001,1000000,1],"x"]`},
		{"very sparse Array, multiple values", multiSparse, `[[-7,21,10,1,20,2],"a","b"]`},
		{"Object", NewObject("foo", "bar", "x-y", "z"), `[{"foo":1,"x-y":2},"bar","z"]`},
		{"Set", NewSet(1, 2, 3), `[["Set",1,2,3],1,2,3]`},
		{"Map", NewMap("a", "b"), `[["Map",1,2],"a","b"]`},
		{"ArrayBuffer", ArrayBuffer([]byte{1, 2, 3}), `[["ArrayBuffer","AQID"]]`},
		{"null prototype object", NewNullProtoObject(), `[["null"]]`},
		{"boxed primitive", boxedNumber, `[["Object",1],42]`},

		// object property order
		{"integer keys hoisted", NewObject("b", 1, "1", 2, "0", 3), `[{"0":1,"1":2,"b":3},3,2,1]`},

		// cycles
		{"cyclical Map", cyclicalMap, `[["Map",1,0],"self"]`},
		{"cyclical Set", cyclicalSet, `[["Set",0,1],42]`},
		{"cyclical Array", cyclicalArray, "[[0]]"},
		{"cyclical Object", cyclicalObject, `[{"self":0}]`},
		{"cyclical null prototype object", cyclicalNullProto, `[["null","self",0]]`},
		{"mutually referential objects", []any{first, second}, `[[1,2],{"second":2},{"first":1}]`},

		// repetition
		{"repeated string", []any{"a string", "a string"}, `[[1,1],"a string"]`},
		{"repeated null", []any{nil, nil}, "[[1,1],null]"},
		{"repeated NaN", []any{math.NaN(), math.NaN()}, "[[-3,-3]]"},
		{"repeated boxed number", []any{boxedNumber, boxedNumber}, `[[1,1],["Object",2],42]`},
		{"repeated boxed bigint", []any{boxedBigInt, boxedBigInt}, `[[1,1],["Object",2],["BigInt","1"]]`},
		{"repeated boxed NaN", []any{boxedNaN, boxedNaN}, `[[1,1],["Object",-3]]`},
		{"repeated object", []any{emptyObject, emptyObject}, "[[1,1],{}]"},
		{"repeated empty Map", []any{emptyMap, emptyMap}, `[[1,1],["Map"]]`},
		{"repeated empty Set", []any{emptySet, emptySet}, `[[1,1],["Set"]]`},
		{"repeated RegExp", []any{regexp, regexp}, `[[1,1],["RegExp","regexp"]]`},
		{"repeated Date", []any{date, date}, `[[1,1],["Date","2001-09-09T01:46:40.000Z"]]`},
	}
}

func holes(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = Hole
	}
	return out
}

func TestStringifyFixtures(t *testing.T) {
	for _, f := range fixtures() {
		got, err := Stringify(f.value)
		if err != nil {
			t.Errorf("%s: Stringify: %v", f.name, err)
			continue
		}
		if got != f.json {
			t.Errorf("%s: Stringify = %s, want %s", f.name, got, f.json)
		}
	}
}

func TestStringifyStrings(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"newline", "a\nb", `["a\nb"]`},
		{"double quotes", string(rune(34)) + "yar" + string(rune(34)), `["` + bs + `"yar` + bs + `""]`},
		{"nul", string(rune(0)), `["` + bs + `u0000"]`},
		{"control character", string(rune(1)), `["` + bs + `u0001"]`},
		{"control character extremum", string(rune(0x1f)), `["` + bs + `u001f"]`},
		{"backslash", bs, `["` + bs + bs + `"]`},
		{"other escapes", "\r\t\b\f", `["\r\t\b\f"]`},
		{"line separator", string(rune(0x2028)), `["` + bs + `u2028"]`},
		{"paragraph separator", string(rune(0x2029)), `["` + bs + `u2029"]`},
		{"dangerous string", "</script><script>", `["` + bs + `u003C/script>` + bs + `u003Cscript>"]`},
		{"surrogate pair", string(rune(0x1D306)), `["` + string(rune(0x1D306)) + `"]`},
	}

	for _, c := range cases {
		got, err := Stringify(c.value)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: Stringify = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestStringifyDangerousKey(t *testing.T) {
	value := NewObject(`<svg onload=alert("xss_works")>`, "bar")
	want := `[{"` + bs + `u003Csvg onload=alert(` + bs + `"xss_works` + bs + `")>":1},"bar"]`

	got, err := Stringify(value)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Stringify = %s, want %s", got, want)
	}
}

func TestFormatNumber(t *testing.T) {
	// Values where Go's default formatting and JavaScript's disagree.
	cases := []struct {
		value float64
		want  string
	}{
		{1, "1"},
		{-1, "-1"},
		{1.5, "1.5"},
		{100, "100"},
		{1e20, "100000000000000000000"},
		{1e21, "1e+21"},
		{1.5e21, "1.5e+21"},
		{1e-6, "0.000001"},
		{1e-7, "1e-7"},
		{0.1, "0.1"},
		{1234567890123, "1234567890123"},
		{9007199254740991, "9007199254740991"},
		{-0.000001, "-0.000001"},
	}

	for _, c := range cases {
		if got := formatNumber(c.value); got != c.want {
			t.Errorf("formatNumber(%v) = %s, want %s", c.value, got, c.want)
		}
	}
}

func TestStringifyRejectsUnsupportedTypes(t *testing.T) {
	type thing struct{ A int }

	_, err := Stringify(thing{A: 1})
	if err == nil {
		t.Fatal("expected an error for a non-POJO")
	}
	if want := "Cannot stringify arbitrary non-POJOs"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

func TestStringifyRejectsProtoKey(t *testing.T) {
	if _, err := Stringify(NewObject("__proto__", 1)); err == nil {
		t.Fatal("expected an error for a __proto__ key")
	}
	if _, err := Stringify(NewNullProtoObject("__proto__", 1)); err == nil {
		t.Fatal("expected an error for a __proto__ key")
	}
}

func TestStringifyGoMapSortsKeys(t *testing.T) {
	got, err := Stringify(map[string]any{"b": 1, "a": 2, "10": 3, "2": 4})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"2":1,"10":2,"a":3,"b":4},4,3,2,1]`
	if got != want {
		t.Errorf("Stringify = %s, want %s", got, want)
	}
}
