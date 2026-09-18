package devalue

import (
	"math"
	"strings"
	"testing"
	"time"
)

// The expected JavaScript in this file is ported from devalue@5.9.2's
// test/index.test.js. The malformed lone-surrogate fixtures are deliberately
// excluded: Go represents them as invalid UTF-8 bytes, and the harvested
// snapshots were not executable JavaScript proof.

// unevalCase is one fixture: a Go value, the JavaScript devalue emits for the
// equivalent JavaScript value, and whether a JS engine can evaluate it.
type unevalCase struct {
	name     string
	value    any
	js       string
	replacer Replacer
	// skipEval marks output that goja cannot evaluate because the runtime lacks
	// the corresponding global (Temporal, URL, or URLSearchParams).
	skipEval string
	// validate runs against the evaluated value in eval_test.go.
	validate string
}

// Foo and Bar stand in for the fixture's custom classes.
type Foo struct{ Value any }
type Bar struct{ Value any }

// jsFunction is raw JavaScript source, the way the reference's function
// fixtures hand a function's `toString()` to the replacer.
type jsFunction string

// notSupported is a value devalue refuses: an arbitrary non-POJO.
type notSupported struct{}

func date(ms int64) Date { return Date(time.UnixMilli(ms).UTC()) }

// sparse builds an array of the given length whose only populated slots are
// the ones in values.
func sparse(length int, values map[int]any) []any {
	arr := make([]any, length)
	for i := range arr {
		arr[i] = Hole
	}
	for i, v := range values {
		arr[i] = v
	}
	return arr
}

func unevalCases(t *testing.T) []unevalCase {
	t.Helper()

	// Shared values, built once so that repetition fixtures reference the same
	// object twice the way the reference's do.
	boxed42 := NewBoxed(42)
	boxedBigInt := NewBoxed(BigInt("1"))
	boxedNaN := NewBoxed(math.NaN())
	emptyObject := NewObject()
	emptyMap := NewMap()
	emptySet := NewSet()
	regexpNoFlags := RegExp{Source: "regexp"}
	dangerousRegExp := RegExp{Source: `[</script><script>alert('xss')//]`}
	sharedDate := date(1e12)
	sharedInstant := Temporal{Kind: TemporalInstant, Value: "1999-09-29T05:30:00Z"}

	tenBytes := ArrayBuffer{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	u8 := NewTypedArray(Uint8Array, tenBytes)
	u16 := NewTypedArray(Uint16Array, tenBytes)
	dv := NewDataView(tenBytes)
	dvSub := NewDataViewRange(tenBytes, 2, 4)
	bigints := BigInt64ArrayOf(1, 2, 3)

	cycleMap := NewMap()
	cycleMap.Set("self", cycleMap)

	cycleSet := NewSet()
	cycleSet.Add(cycleSet)
	cycleSet.Add(42)

	cycleArray := make([]any, 1)
	cycleArray[0] = cycleArray

	cycleObject := NewObject()
	cycleObject.Set("self", cycleObject)

	cycleNullProto := NewNullProtoObject()
	cycleNullProto.Set("self", cycleNullProto)

	cycleWithProperty := NewObject("foo", "bar")
	cycleWithProperty.Set("self", cycleWithProperty)

	first, second := NewObject(), NewObject()
	first.Set("second", second)
	second.Set("first", first)

	mapKey := NewObject("id", 1)

	node1, node2, node3 := NewObject("id", 1), NewObject("id", 2), NewObject("id", 3)
	interlinked := NewMap(
		node1, NewMap(node2, 1, node3, 1),
		node2, NewMap(node1, 1, node3, 1),
		node3, NewMap(node1, 1, node2, 1),
	)

	fooInstance := &Foo{Value: NewObject("bar", &Bar{Value: NewObject("answer", 42)})}
	fooReplacer := func(v any, uneval func(any) (string, error)) (string, bool, error) {
		switch t := v.(type) {
		case *Foo:
			inner, err := uneval(t.Value)
			if err != nil {
				return "", false, err
			}
			return "new Foo(" + inner + ")", true, nil
		case *Bar:
			inner, err := uneval(t.Value)
			if err != nil {
				return "", false, err
			}
			return "new Bar(" + inner + ")", true, nil
		}
		return "", false, nil
	}

	return []unevalCase{
		// --- primitives ---
		{name: "number: positive integer", value: 42, js: `42`},
		{name: "number: negative integer", value: -5, js: `-5`},
		{name: "number: positive decimal", value: 0.1, js: `.1`},
		{name: "number: negative decimal", value: -0.1, js: `-.1`},
		{name: "number: NaN", value: math.NaN(), js: `NaN`},
		{name: "number: +Infinity", value: math.Inf(1), js: `Infinity`},
		{name: "number: -Infinity", value: math.Inf(-1), js: `-Infinity`},
		{name: "number: zero", value: 0, js: `0`},
		{name: "number: negative zero", value: math.Copysign(0, -1), js: `-0`, validate: "Object.is($, -0)"},
		{name: "string", value: "woo!!!", js: `"woo!!!"`},
		{name: "boolean", value: true, js: `true`},
		{name: "bigint", value: BigInt("1"), js: `1n`},
		{name: "undefined", value: Undefined, js: `void 0`},
		{name: "null", value: nil, js: `null`},

		// --- boxed primitives ---
		{name: "Number: positive integer", value: NewBoxed(42), js: `Object(42)`},
		{name: "Number: negative integer", value: NewBoxed(-2), js: `Object(-2)`},
		{name: "Number: positive decimal", value: NewBoxed(0.1), js: `Object(.1)`},
		{name: "Number: negative decimal", value: NewBoxed(-0.1), js: `Object(-.1)`},
		{name: "Number: NaN", value: NewBoxed(math.NaN()), js: `Object(NaN)`},
		{name: "Number: +Infinity", value: NewBoxed(math.Inf(1)), js: `Object(Infinity)`},
		{name: "Number: -Infinity", value: NewBoxed(math.Inf(-1)), js: `Object(-Infinity)`},
		{name: "Number: zero", value: NewBoxed(0), js: `Object(0)`},
		{
			name: "Number: negative zero", value: NewBoxed(math.Copysign(0, -1)), js: `Object(-0)`,
			validate: "typeof $ === 'object' && Object.is($.valueOf(), -0)",
		},
		{name: "String", value: NewBoxed("woo!!!"), js: `Object("woo!!!")`},
		{name: "Boolean", value: NewBoxed(true), js: `Object(true)`},
		{name: "BigInt", value: NewBoxed(BigInt("1")), js: `Object(1n)`},

		// --- basics ---
		{name: "RegExp", value: RegExp{Source: "regexp", Flags: "gim"}, js: `new RegExp("regexp","gim")`},
		{name: "Date", value: date(1e12), js: `new Date(1000000000000)`, validate: "$.getTime() === 1000000000000"},
		{name: "Array", value: []any{"a", "b", "c"}, js: `["a","b","c"]`},
		{
			name:  "Array where negative zero appears after normal zero",
			value: []any{0, math.Copysign(0, -1)},
			js:    `[0,-0]`,
		},
		{name: "Array (empty)", value: []any{}, js: `[]`},
		{name: "Array (sparse)", value: []any{Hole, "b", Hole}, js: `[,"b",,]`},
		{
			name:     "Array (very sparse)",
			value:    sparse(1000001, map[int]any{1000000: "x"}),
			js:       `Object.assign(Array(1000001),{1000000:"x"})`,
			validate: "$.length === 1000001 && $[1000000] === 'x' && !(0 in $) && !(999999 in $)",
		},
		{
			name:     "Array (very sparse, multiple values)",
			value:    sparse(21, map[int]any{10: "a", 20: "b"}),
			js:       `[,,,,,,,,,,"a",,,,,,,,,,"b"]`,
			validate: "$.length === 21 && $[10] === 'a' && $[20] === 'b' && !(0 in $) && !(9 in $) && !(11 in $)",
		},
		{name: "Object", value: NewObject("foo", "bar", "x-y", "z"), js: `{foo:"bar","x-y":"z"}`},
		{name: "Set", value: NewSet(1, 2, 3), js: `new Set([1,2,3])`},
		{name: "Map", value: NewMap("a", "b"), js: `new Map([["a","b"]])`},
		{name: "Uint8Array", value: Uint8ArrayOf(1, 2, 3), js: `new Uint8Array([1,2,3])`},
		{name: "Node Buffer", value: Uint8ArrayOf(65, 65, 65, 65), js: `new Uint8Array([65,65,65,65])`},
		{
			name:     "Float64Array with negative zero",
			value:    Float64ArrayOf(math.Copysign(0, -1), 1.5),
			js:       `new Float64Array([-0,1.5])`,
			validate: "Object.is($[0], -0) && $[1] === 1.5",
		},
		{
			name: "BigInt64Array", value: BigInt64ArrayOf(1, -2, 3),
			js: `new BigInt64Array([1n,-2n,3n])`,
		},
		{
			name: "BigUint64Array", value: BigUint64ArrayOf(1, 2, 3),
			js: `new BigUint64Array([1n,2n,3n])`,
		},
		{name: "ArrayBuffer", value: ArrayBuffer{1, 2, 3}, js: `new Uint8Array([1,2,3]).buffer`},
		{name: "DataView", value: NewDataView(ArrayBuffer{1, 2, 3}), js: `new DataView(new Uint8Array([1,2,3]).buffer)`},
		{
			name:  "DataView subview",
			value: NewDataViewRange(tenBytes, 2, 4),
			js:    `new DataView(new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer,2,4)`,
		},
		{
			name:     "URL",
			value:    URL("https://user:password@example.com/%3Cscript%3E/path?foo=bar#hash"),
			js:       `new URL("https://user:password@example.com/%3Cscript%3E/path?foo=bar#hash")`,
			skipEval: "goja has no URL",
		},
		{
			name:     "URLSearchParams",
			value:    URLSearchParams("foo=1&foo=2&baz=%3C+%3E"),
			js:       `new URLSearchParams("foo=1&foo=2&baz=%3C+%3E")`,
			skipEval: "goja has no URLSearchParams",
		},
		{
			name:     "Sliced typed array",
			value:    Uint16ArrayOf(10, 20, 30, 40).Subarray(1, 3),
			js:       `new Uint16Array([10,20,30,40]).subarray(1,3)`,
			validate: "$.length === 2 && $[0] === 20 && $[1] === 30",
		},
		{
			name:     "Temporal.Duration",
			value:    Temporal{Kind: TemporalDuration, Value: "P1Y2M3D"},
			js:       `Temporal.Duration.from("P1Y2M3D")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.Instant",
			value:    Temporal{Kind: TemporalInstant, Value: "1999-09-29T05:30:00Z"},
			js:       `Temporal.Instant.from("1999-09-29T05:30:00Z")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.PlainDate",
			value:    Temporal{Kind: TemporalPlainDate, Value: "1999-09-29"},
			js:       `Temporal.PlainDate.from("1999-09-29")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.PlainTime",
			value:    Temporal{Kind: TemporalPlainTime, Value: "12:34:56"},
			js:       `Temporal.PlainTime.from("12:34:56")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.PlainDateTime",
			value:    Temporal{Kind: TemporalPlainDateTime, Value: "1999-09-29T12:34:56"},
			js:       `Temporal.PlainDateTime.from("1999-09-29T12:34:56")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.PlainMonthDay",
			value:    Temporal{Kind: TemporalPlainMonthDay, Value: "09-29"},
			js:       `Temporal.PlainMonthDay.from("09-29")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.PlainYearMonth",
			value:    Temporal{Kind: TemporalPlainYearMonth, Value: "1999-09"},
			js:       `Temporal.PlainYearMonth.from("1999-09")`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Temporal.ZonedDateTime",
			value:    Temporal{Kind: TemporalZonedDateTime, Value: "1999-09-29T12:34:56+02:00[Europe/Rome]"},
			js:       `Temporal.ZonedDateTime.from("1999-09-29T12:34:56+02:00[Europe/Rome]")`,
			skipEval: "goja has no Temporal",
		},

		// --- strings ---
		{name: "newline", value: "a\nb", js: `"a\nb"`},
		{name: "double quotes", value: `"yar"`, js: `"\"yar\""`},
		{name: "surrogate pair", value: "𝌆", js: `"𝌆"`},
		{name: "nul", value: "\x00", js: `"\u0000"`},
		{name: "control character", value: "\x01", js: `"\u0001"`},
		{name: "control character extremum", value: "\x1f", js: `"\u001f"`},
		{name: "backslash", value: `\`, js: `"\\"`},

		// --- cycles ---
		{
			name: "Map (cyclical)", value: cycleMap,
			js:       `(function(a){a.set("self", a);return a}(new Map))`,
			validate: "$.get('self') === $",
		},
		{
			name: "Set (cyclical)", value: cycleSet,
			js:       `(function(a){a.add(a).add(42);return a}(new Set))`,
			validate: "$.size === 2 && $.has(42) && $.has($)",
		},
		{
			name: "Array (cyclical)", value: cycleArray,
			js:       `(function(a){a[0]=a;return a}(Array(1)))`,
			validate: "$.length === 1 && $[0] === $",
		},
		{
			name: "Object (cyclical)", value: cycleObject,
			js:       `(function(a){a.self=a;return a}({}))`,
			validate: "$.self === $",
		},
		{
			name: "Object with null prototype (cyclical)", value: cycleNullProto,
			js:       `(function(a){a.self=a;return a}(Object.create(null)))`,
			validate: "Object.getPrototypeOf($) === null && $.self === $",
		},
		{
			name: "Object with null prototype class", value: cycleWithProperty,
			js:       `(function(a){a.foo="bar";a.self=a;return a}({}))`,
			validate: "$.foo === 'bar' && $.self === $",
		},
		{
			name: "Object (cyclical, pair)", value: []any{first, second},
			js:       `(function(a,b){a.second=b;b.first=a;return [a,b]}({},{}))`,
			validate: "$[0].second === $[1] && $[1].first === $[0]",
		},

		// --- repetition ---
		{name: "string (repetition)", value: []any{"a string", "a string"}, js: `["a string","a string"]`},
		{name: "null (repetition)", value: []any{nil, nil}, js: `[null,null]`},
		{name: "number: NaN (repetition)", value: []any{math.NaN(), math.NaN()}, js: `[NaN,NaN]`},
		{
			name: "Number (repetition)", value: []any{boxed42, boxed42},
			js: `(function(a){return [a,a]}(Object(42)))`, validate: "$[0] === $[1]",
		},
		{
			name: "BigInt (repetition)", value: []any{boxedBigInt, boxedBigInt},
			js: `(function(a){return [a,a]}(Object(1n)))`,
		},
		{
			name: "Number: NaN (repetition)", value: []any{boxedNaN, boxedNaN},
			js: `(function(a){return [a,a]}(Object(NaN)))`, validate: "$[0] === $[1]",
		},
		{
			name: "Object (repetition)", value: []any{emptyObject, emptyObject},
			js: `(function(a){return [a,a]}({}))`, validate: "$[0] === $[1]",
		},
		{
			name: "empty Map (repetition)", value: []any{emptyMap, emptyMap},
			js: `(function(a){return [a,a]}(new Map))`, validate: "$[0] === $[1] && $[0].size === 0",
		},
		{
			name: "empty Set (repetition)", value: []any{emptySet, emptySet},
			js: `(function(a){return [a,a]}(new Set))`, validate: "$[0] === $[1] && $[0].size === 0",
		},
		{
			name: "RegExp (repetition)", value: []any{regexpNoFlags, regexpNoFlags},
			js: `(function(a){return [a,a]}(new RegExp("regexp")))`, validate: "$[0] === $[1]",
		},
		{
			name: "Date (repetition)", value: []any{sharedDate, sharedDate},
			js: `(function(a){return [a,a]}(new Date(1000000000000)))`, validate: "$[0] === $[1]",
		},
		{
			name:     "Array buffer (repetition)",
			value:    []any{u8, u16},
			js:       `(function(a){return [new Uint8Array(a),new Uint16Array(a)]}(new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer))`,
			validate: "$[0].buffer === $[1].buffer",
		},
		{
			name:     "TypedArray (repetition)",
			value:    []any{u8, u8},
			js:       `(function(a){a=new Uint8Array([0,1,2,3,4,5,6,7,8,9]);return [a,a]}({}))`,
			validate: "$[0] === $[1] && $[0][9] === 9",
		},
		{
			name:     "Array Buffer and TypedArray (repetition)",
			value:    []any{u8, u8, u16},
			js:       `(function(a,b){a=new Uint8Array(b);return [a,a,new Uint16Array(b)]}({},new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer))`,
			validate: "$[0] === $[1] && $[0].buffer === $[2].buffer",
		},
		{
			name:     "DataView (repetition)",
			value:    []any{dv, dv},
			js:       `(function(a){a=new DataView(new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer);return [a,a]}({}))`,
			validate: "$[0] === $[1] && $[0].byteLength === 10",
		},
		{
			name:     "Array Buffer and DataView (repetition)",
			value:    []any{dv, dv, tenBytes},
			js:       `(function(a,b){a=new DataView(b);return [a,a,b]}({},new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer))`,
			validate: "$[0] === $[1] && $[0].buffer === $[2]",
		},
		{
			name:     "DataView subview (repetition)",
			value:    []any{dvSub, dvSub},
			js:       `(function(a){a=new DataView(new Uint8Array([0,1,2,3,4,5,6,7,8,9]).buffer,2,4);return [a,a]}({}))`,
			validate: "$[0] === $[1] && $[0].byteOffset === 2 && $[0].byteLength === 4",
		},
		{
			name: "BigInt64Array (repetition)", value: []any{bigints, bigints},
			js: `(function(a){a=new BigInt64Array([1n,2n,3n]);return [a,a]}({}))`,
		},
		{
			name: "Temporal.Instant (repetition)", value: []any{sharedInstant, sharedInstant},
			js:       `(function(a){return [a,a]}(Temporal.Instant.from("1999-09-29T05:30:00Z")))`,
			skipEval: "goja has no Temporal",
		},
		{
			name:     "Map key (repetition)",
			value:    []any{mapKey, NewMap(mapKey, "v")},
			js:       `(function(a){a.id=1;return [a,new Map([[a,"v"]])]}({}))`,
			validate: "Array.from($[1].keys())[0] === $[0]",
		},
		{
			name:  "Map keys (interlinked)",
			value: interlinked,
			js:    `(function(a,b,c){a.id=1;b.id=2;c.id=3;return new Map([[a,new Map([[b,1],[c,1]])],[b,new Map([[a,1],[c,1]])],[c,new Map([[a,1],[b,1]])]])}({},{},{}))`,
			validate: "(function(m){var k=Array.from(m.keys());" +
				"var f1=Array.from(m.get(k[0]).keys()),f2=Array.from(m.get(k[1]).keys()),f3=Array.from(m.get(k[2]).keys());" +
				"return f2[0]===k[0]&&f3[0]===k[0]&&f1[0]===k[1]&&f3[1]===k[1]&&f1[1]===k[2]&&f2[1]===k[2]})($)",
		},

		// --- XSS ---
		{
			name:  "Dangerous string",
			value: `</script><script src='https://evil.com/script.js'>alert('pwned')</script><script>`,
			js:    `"\u003C/script>\u003Cscript src='https://evil.com/script.js'>alert('pwned')\u003C/script>\u003Cscript>"`,
		},
		{
			name:  "Dangerous key",
			value: NewObject(`<svg onload=alert("xss_works")>`, "bar"),
			js:    `{"\u003Csvg onload=alert(\"xss_works\")>":"bar"}`,
		},
		{
			name:  "Dangerous regex",
			value: dangerousRegExp,
			js:    `new RegExp("[\u003C/script>\u003Cscript>alert('xss')//]")`,
		},
		{
			name:  "Dangerous regex (repetition)",
			value: []any{dangerousRegExp, dangerousRegExp},
			js:    `(function(a){return [a,a]}(new RegExp("[\u003C/script>\u003Cscript>alert('xss')//]")))`,
		},

		// --- misc ---
		{
			name: "Object without prototype", value: NewNullProtoObject(), js: `{__proto__:null}`,
			validate: "Object.getPrototypeOf($) === null && Object.keys($).length === 0",
		},
		{
			name: "cross-realm POJO", value: NewObject(), js: `{}`,
			validate: "Object.getPrototypeOf($) === Object.prototype && Object.keys($).length === 0",
		},
		{name: "non-enumerable symbolic key", value: NewObject("x", 1), js: `{x:1}`},

		// --- custom (replacer) ---
		{
			name:     "Custom type",
			value:    []any{fooInstance, fooInstance},
			js:       `(function(a){return [a,a]}(new Foo({bar:new Bar({answer:42})})))`,
			replacer: fooReplacer,
		},
		{
			name:  "Custom fallback",
			value: date(0),
			js:    `new Date('')`,
			replacer: func(v any, _ func(any) (string, error)) (string, bool, error) {
				if _, ok := v.(Date); ok {
					return `new Date('')`, true, nil
				}
				return "", false, nil
			},
			validate: "isNaN($.getTime())",
		},
		{
			name:  "Function wrapped in custom type",
			value: &Foo{Value: jsFunction("(x) => x * 2")},
			js:    `new FunctionRef((x) => x * 2)`,
			replacer: func(v any, _ func(any) (string, error)) (string, bool, error) {
				switch t := v.(type) {
				case *Foo:
					return "new FunctionRef(" + string(t.Value.(jsFunction)) + ")", true, nil
				case jsFunction:
					return string(t), true, nil
				}
				return "", false, nil
			},
		},
		{
			name:  "Function in nested structure",
			value: NewObject("fn", jsFunction("(x) => x * 2"), "nested", NewObject("data", 42)),
			js:    `{fn:(x) => x * 2,nested:{data:42}}`,
			replacer: func(v any, _ func(any) (string, error)) (string, bool, error) {
				if t, ok := v.(jsFunction); ok {
					return string(t), true, nil
				}
				return "", false, nil
			},
			validate: "typeof $.fn === 'function' && $.nested.data === 42 && $.fn(3) === 6",
		},
	}
}

func TestUneval(t *testing.T) {
	for _, c := range unevalCases(t) {
		t.Run(c.name, func(t *testing.T) {
			got, err := UnevalWith(c.value, c.replacer)
			if err != nil {
				t.Fatalf("UnevalWith: %v", err)
			}
			if got != c.js {
				t.Errorf("\n got: %s\nwant: %s", got, c.js)
			}
		})
	}
}

func TestNewUnevalValuesRemainUnsupportedByFlatStringify(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{"URL", URL("https://example.com/")},
		{"URLSearchParams", URLSearchParams("a=1")},
		{"Temporal", Temporal{Kind: TemporalInstant, Value: "1999-09-29T05:30:00Z"}},
		{"TypedArray", Uint8ArrayOf(1, 2, 3)},
		{"DataView", NewDataView(ArrayBuffer{1, 2, 3})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Stringify(tt.value); err == nil {
				t.Fatal("Stringify unexpectedly accepted an Uneval-only value")
			}
		})
	}
}

func TestNewUnevalValueIdentityInMapAndSet(t *testing.T) {
	view := Uint8ArrayOf(1, 2, 3)
	set := NewSet(view, view)
	if set.Len() != 1 {
		t.Fatalf("Set length = %d, want 1", set.Len())
	}

	url := URL("https://example.com/")
	m := NewMap(url, "first", url, "second")
	if m.Len() != 1 {
		t.Fatalf("Map length = %d, want 1", m.Len())
	}
	if got, _ := m.Get(url); got != "second" {
		t.Fatalf("Map value = %v, want second", got)
	}
}

func TestUnevalErrors(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		message string
		path    string
	}{
		{
			name:    "non-POJO",
			value:   notSupported{},
			message: "Cannot stringify arbitrary non-POJOs",
		},
		{
			name:    "__proto__ key",
			value:   NewObject("foo", NewObject("__proto__", 1)),
			message: "Cannot stringify objects with __proto__ keys",
			path:    ".foo",
		},
		{
			name:    "populates path",
			value:   NewObject("foo", NewObject("array", []any{notSupported{}})),
			message: "Cannot stringify arbitrary non-POJOs",
			path:    ".foo.array[0]",
		},
		{
			name:    "populates path through a Map",
			value:   NewObject("foo", NewObject("string-key", NewMap("key", notSupported{}))),
			message: "Cannot stringify arbitrary non-POJOs",
			path:    `.foo["string-key"].get("key")`,
		},
		{
			// devalue #64: the path must not keep a Map's key after the Map.
			name:    "populates path after maps",
			value:   NewObject("map", NewMap("key", "value"), "object", NewObject("invalid", notSupported{})),
			message: "Cannot stringify arbitrary non-POJOs",
			path:    ".object.invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Uneval(tt.value)
			if err == nil {
				t.Fatal("expected an error")
			}
			de, ok := err.(*DevalueError)
			if !ok {
				t.Fatalf("expected a *DevalueError, got %T: %v", err, err)
			}
			if de.Message != tt.message {
				t.Errorf("message: got %q, want %q", de.Message, tt.message)
			}
			if de.Path != tt.path {
				t.Errorf("path: got %q, want %q", de.Path, tt.path)
			}
		})
	}
}

// TestUnevalIgnoresNothingInSparseArrays is the Go shape of the reference's
// "ignores non-numeric array properties" tests. A Go []any cannot carry a
// non-index property at all, so what remains testable is that the two sparse
// encodings are chosen by the cost rule and contain only indices.
func TestUnevalSparseEncodingChoice(t *testing.T) {
	dense, err := Uneval([]any{Hole, "a", Hole, "b"})
	if err != nil {
		t.Fatal(err)
	}
	if dense != `[,"a",,"b"]` {
		t.Errorf("dense: got %s", dense)
	}

	verySparse, err := Uneval(sparse(1000001, map[int]any{1000000: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verySparse, "Object.assign") {
		t.Errorf("very sparse: expected Object.assign, got %s", verySparse)
	}
}

// TestUnevalEmptyContainersAreNotShared guards the one place this package
// departs from devalue's reference identity: Go cannot tell two empty slices
// apart, so an empty array, buffer or map is never hoisted. Collapsing them
// would alias `{a:[],b:[]}` into one array in the hydrated client.
func TestUnevalEmptyContainersAreNotShared(t *testing.T) {
	got, err := Uneval(NewObject("a", []any{}, "b", []any{}))
	if err != nil {
		t.Fatal(err)
	}
	if got != `{a:[],b:[]}` {
		t.Fatalf("got %s, want {a:[],b:[]}", got)
	}

	vm, v := evalJS(t, got)
	assertJS(t, vm, v, `$.a !== $.b && $.a.length === 0 && $.b.length === 0`)
}

// TestUnevalReplacerSeesEmptyContainers: an empty container is not tracked, so
// it takes the replacer path in stringify rather than in walk. It still has to
// reach the replacer.
func TestUnevalReplacerSeesEmptyContainers(t *testing.T) {
	replacer := func(v any, _ func(any) (string, error)) (string, bool, error) {
		if arr, ok := v.([]any); ok && len(arr) == 0 {
			return "EMPTY", true, nil
		}
		return "", false, nil
	}
	got, err := UnevalWith(NewObject("a", []any{}), replacer)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{a:EMPTY}` {
		t.Fatalf("got %s, want {a:EMPTY}", got)
	}
}

// TestUnevalSparseCostBoundary pins the exact length at which devalue's cost
// rule flips from a holey array literal to Object.assign, for one, two and
// three populated slots. The reference's own fixtures only sit at the extremes,
// so the constants in the rule can be wrong by several characters and still
// pass them; these expectations were produced by running devalue's own
// `uneval` over the same arrays.
func TestUnevalSparseCostBoundary(t *testing.T) {
	// build makes an array of the given length populated at every second index.
	build := func(length, population int) []any {
		arr := make([]any, length)
		for i := range arr {
			arr[i] = Hole
		}
		for i := range population {
			arr[i*2] = "v" + itoa(i)
		}
		return arr
	}

	tests := []struct {
		length     int
		population int
		js         string
	}{
		{29, 1, `["v0",,,,,,,,,,,,,,,,,,,,,,,,,,,,,]`},
		{30, 1, `Object.assign(Array(30),{0:"v0"})`},
		{33, 2, `["v0",,"v1",,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,]`},
		{34, 2, `Object.assign(Array(34),{0:"v0",2:"v1"})`},
		{37, 3, `["v0",,"v1",,"v2",,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,]`},
		{38, 3, `Object.assign(Array(38),{0:"v0",2:"v1",4:"v2"})`},
	}

	for _, tt := range tests {
		t.Run(itoa(tt.length)+"/"+itoa(tt.population), func(t *testing.T) {
			got, err := Uneval(build(tt.length, tt.population))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.js {
				t.Errorf("\n got: %s\nwant: %s", got, tt.js)
			}
		})
	}
}

// TestUnevalDoesNotCreateDuplicateParameterNames is the reference test of the
// same name: 20000 shared values must get 20000 distinct hoisted names.
func TestUnevalDoesNotCreateDuplicateParameterNames(t *testing.T) {
	foo := make([]any, 20000)
	for i := range foo {
		foo[i] = i
	}
	value := []any{foo}
	for i := range foo {
		value = append(value, NewObject(itoa(i), foo[i]))
	}
	// Every element of `foo` also appears in one of the objects, so 20000
	// numbers repeat — but numbers are primitives and are never hoisted. What
	// repeats structurally is `foo` itself, referenced once. Force the names
	// by sharing each object twice.
	shared := make([]any, 0, len(value)*2)
	shared = append(shared, value...)
	shared = append(shared, value...)

	out, err := Uneval(shared)
	if err != nil {
		t.Fatal(err)
	}
	params := unevalParams(t, out)
	seen := make(map[string]bool, len(params))
	for _, p := range params {
		if seen[p] {
			t.Fatalf("duplicate parameter name %q", p)
		}
		seen[p] = true
	}
	if len(params) < 20000 {
		t.Fatalf("expected at least 20000 hoisted names, got %d", len(params))
	}
}

// TestUnevalLargeGraph is the reference's "serializes more than 65534 repeated
// references to valid JS": past 65534 hoisted values the IIFE must take a
// single array argument and destructure it, because a function may have at
// most 65535 parameters.
func TestUnevalLargeGraph(t *testing.T) {
	shared := make([]any, 70000)
	for i := range shared {
		shared[i] = NewObject("i", i)
	}
	copied := make([]any, len(shared))
	copy(copied, shared)

	out, err := Uneval(NewObject("a", shared, "b", copied))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "(function(){var[") {
		t.Fatalf("expected the destructuring form, got %.60s...", out)
	}
	if !strings.Contains(out, "]=arguments[0];") {
		t.Fatalf("expected `=arguments[0];`, got %.120s...", out)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// unevalParams extracts the parameter list of the emitted IIFE.
func unevalParams(t *testing.T, out string) []string {
	t.Helper()
	const prefix = "(function("
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("not an IIFE: %.60s...", out)
	}
	end := strings.Index(out, "){")
	if end < 0 {
		t.Fatal("malformed IIFE")
	}
	return strings.Split(out[len(prefix):end], ",")
}
