package devalue

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// The emitted expression is JavaScript, so the only proof that it is correct
// is a JavaScript engine evaluating it. These tests run every fixture's output
// through goja and assert on the value that comes back — in particular that a
// cycle really is a cycle (`a.self === a`), which no string comparison can
// show.

// evalJS evaluates one uneval expression and returns the resulting value.
func evalJS(t *testing.T, expr string) (*goja.Runtime, goja.Value) {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(`
		function Foo(value) { this.value = value }
		function Bar(value) { this.value = value }
		function FunctionRef(fn) { return fn }
	`); err != nil {
		t.Fatal(err)
	}
	v, err := vm.RunString("(" + expr + ")")
	if err != nil {
		t.Fatalf("evaluating %s: %v", expr, err)
	}
	return vm, v
}

// assertJS evaluates `check` with `$` bound to value and requires it to be true.
func assertJS(t *testing.T, vm *goja.Runtime, value goja.Value, check string) {
	t.Helper()
	if err := vm.Set("$", value); err != nil {
		t.Fatal(err)
	}
	res, err := vm.RunString("!!(" + check + ")")
	if err != nil {
		t.Fatalf("evaluating check %s: %v", check, err)
	}
	if !res.ToBoolean() {
		t.Errorf("check failed: %s", check)
	}
}

func TestUnevalEvaluates(t *testing.T) {
	evaluated := 0
	for _, c := range unevalCases(t) {
		t.Run(c.name, func(t *testing.T) {
			if c.skipEval != "" {
				// A skip that nobody counts is a silent hole, so the parent
				// test asserts how many cases actually ran.
				t.Skipf("not evaluated: %s", c.skipEval)
			}
			out, err := UnevalWith(c.value, c.replacer)
			if err != nil {
				t.Fatalf("UnevalWith: %v", err)
			}
			vm, v := evalJS(t, out)
			if c.validate != "" {
				assertJS(t, vm, v, c.validate)
			}
			evaluated++
		})
	}

	// Guards against skips quietly expanding. Only globals absent from goja are
	// skipped; BigInt and replacer fixtures execute.
	if evaluated != 91 {
		t.Fatalf("evaluated %d fixtures in a JS engine, want 91", evaluated)
	}
}

// TestUnevalRoundTripsThroughJS checks that the values that survive a round
// trip through the engine equal what went in, using expectations written into
// the test rather than read back off the emitter.
func TestUnevalRoundTripsThroughJS(t *testing.T) {
	tests := []struct {
		name  string
		value any
		check string
	}{
		{"object", NewObject("a", 1, "b", "two", "c", true), `$.a === 1 && $.b === 'two' && $.c === true`},
		{"nested", NewObject("list", []any{1, 2, 3}, "inner", NewObject("x", "y")),
			`$.list.length === 3 && $.list[2] === 3 && $.inner.x === 'y'`},
		{"undefined property", NewObject("u", Undefined), `'u' in $ && $.u === undefined`},
		{"map", NewMap("a", 1, "b", 2), `$.size === 2 && $.get('a') === 1 && $.get('b') === 2`},
		{"set", NewSet("a", "b"), `$.size === 2 && $.has('a') && $.has('b')`},
		{"regexp", RegExp{Source: "a+b", Flags: "gi"}, `$.source === 'a+b' && $.flags === 'gi'`},
		{"date", date(1234567890123), `$.getTime() === 1234567890123`},
		{"null proto", NewNullProtoObject("k", "v"), `Object.getPrototypeOf($) === null && $.k === 'v'`},
		{"numbers", []any{1e21, 1e-7, 0.1, -0.1, 1 << 30},
			`$[0] === 1e21 && $[1] === 1e-7 && $[2] === 0.1 && $[3] === -0.1 && $[4] === 1073741824`},
		{"escapes", "</script>  \t\x00", `$.length === 13 && $.charCodeAt(9) === 0x2028 && $.charCodeAt(12) === 0`},
		{"typed array", Uint16ArrayOf(1, 300, 65535),
			`$ instanceof Uint16Array && $.length === 3 && $[1] === 300 && $[2] === 65535`},
		{"float array", Float32ArrayOf(1.5, -2.25), `$[0] === 1.5 && $[1] === -2.25`},
		{"array buffer", ArrayBuffer{9, 8, 7},
			`$.byteLength === 3 && new Uint8Array($)[0] === 9 && new Uint8Array($)[2] === 7`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Uneval(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			vm, v := evalJS(t, out)
			assertJS(t, vm, v, tt.check)
		})
	}
}

// TestUnevalCyclesEvaluateToCycles is the property the IIFE exists for.
func TestUnevalCyclesEvaluateToCycles(t *testing.T) {
	t.Run("object self reference", func(t *testing.T) {
		o := NewObject("name", "root")
		o.Set("self", o)
		out, err := Uneval(o)
		if err != nil {
			t.Fatal(err)
		}
		vm, v := evalJS(t, out)
		assertJS(t, vm, v, `$.self === $ && $.self.self.self.name === 'root'`)
	})

	t.Run("mutual reference", func(t *testing.T) {
		a, b := NewObject("id", "a"), NewObject("id", "b")
		a.Set("peer", b)
		b.Set("peer", a)
		out, err := Uneval([]any{a, b})
		if err != nil {
			t.Fatal(err)
		}
		vm, v := evalJS(t, out)
		assertJS(t, vm, v, `$[0].peer === $[1] && $[1].peer === $[0] && $[0].id === 'a'`)
	})

	t.Run("array through map", func(t *testing.T) {
		arr := make([]any, 2)
		m := NewMap("items", arr)
		arr[0] = m
		arr[1] = "tail"
		out, err := Uneval(m)
		if err != nil {
			t.Fatal(err)
		}
		vm, v := evalJS(t, out)
		assertJS(t, vm, v, `$.get('items')[0] === $ && $.get('items')[1] === 'tail'`)
	})

	// devalue's "reconstructs a referenced typed array before it is used in a
	// cycle": the typed array's reconstruction must run before the statements
	// that reference it, or they capture the `{}` placeholder.
	t.Run("typed array in a cycle", func(t *testing.T) {
		view := Uint8ArrayOf(1, 2, 3)
		obj := NewObject("a", view, "b", view)
		obj.Set("self", obj)
		out, err := Uneval(obj)
		if err != nil {
			t.Fatal(err)
		}
		vm, v := evalJS(t, out)
		assertJS(t, vm, v,
			`$.self === $ && $.a instanceof Uint8Array && $.a === $.b && $.a[0] === 1 && $.a[2] === 3`)
	})
}

// TestUnevalSparseArraysEvaluate is devalue's "uneval round-trips sparse arrays
// whose first hole is not at index 0": the trailing-comma rule and the hole
// literal both have to survive the engine, which is where a length off by one
// would show up.
func TestUnevalSparseArraysEvaluate(t *testing.T) {
	tests := []struct {
		name  string
		value []any
		check string
	}{
		{"[1,,3]", []any{1, Hole, 3}, `$.length === 3 && $[0] === 1 && !(1 in $) && $[2] === 3`},
		{"[1,,]", []any{1, Hole}, `$.length === 2 && $[0] === 1 && !(1 in $)`},
		{"[1,,,4]", []any{1, Hole, Hole, 4}, `$.length === 4 && $[0] === 1 && !(1 in $) && !(2 in $) && $[3] === 4`},
		{"[1,2,,4]", []any{1, 2, Hole, 4}, `$.length === 4 && $[1] === 2 && !(2 in $) && $[3] === 4`},
		{"leading hole", []any{Hole, "b", Hole}, `$.length === 3 && !(0 in $) && $[1] === 'b' && !(2 in $)`},
		{"all holes", []any{Hole, Hole, Hole}, `$.length === 3 && Object.keys($).length === 0`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Uneval(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			vm, v := evalJS(t, out)
			assertJS(t, vm, v, tt.check)
		})
	}

	t.Run("Object.assign form", func(t *testing.T) {
		out, err := Uneval(sparse(100000, map[int]any{99999: "x"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out, "Object.assign(") {
			t.Fatalf("expected the Object.assign form, got %.60s...", out)
		}
		vm, v := evalJS(t, out)
		assertJS(t, vm, v, `$.length === 100000 && $[99999] === 'x' && !(0 in $) && Object.keys($).length === 1`)
	})
}

// TestUnevalLargeGraphEvaluates is devalue's "serializes more than 65534
// repeated references to valid JS": the destructuring fallback has to be
// something an engine will actually parse and run.
func TestUnevalLargeGraphEvaluates(t *testing.T) {
	if testing.Short() {
		t.Skip("large graph")
	}
	const n = 70000
	shared := make([]any, n)
	for i := range shared {
		shared[i] = NewObject("i", i)
	}
	copied := make([]any, n)
	copy(copied, shared)

	out, err := Uneval(NewObject("a", shared, "b", copied))
	if err != nil {
		t.Fatal(err)
	}
	vm, v := evalJS(t, out)
	assertJS(t, vm, v,
		`$.a.length === 70000 && $.a[0].i === 0 && $.a[69999].i === 69999 && $.a[123] === $.b[123]`)
}

// TestUnevalReplacerEvaluates covers the shape SvelteKit's transport uses: the
// replacer emits a call to a decoder, with the encoded payload produced by the
// nested emitter.
func TestUnevalReplacerEvaluates(t *testing.T) {
	type vector struct{ X, Y float64 }

	replacer := func(v any, uneval func(any) (string, error)) (string, bool, error) {
		vec, ok := v.(*vector)
		if !ok {
			return "", false, nil
		}
		payload, err := uneval([]any{vec.X, vec.Y})
		if err != nil {
			return "", false, err
		}
		return `app.decode("vector", ` + payload + `)`, true, nil
	}

	out, err := UnevalWith(NewObject("at", &vector{X: 1.5, Y: -0.25}, "label", "origin"), replacer)
	if err != nil {
		t.Fatal(err)
	}
	want := `{at:app.decode("vector", [1.5,-.25]),label:"origin"}`
	if out != want {
		t.Fatalf("\n got: %s\nwant: %s", out, want)
	}

	vm := goja.New()
	if _, err := vm.RunString(`var app = { decode: function (name, raw) { return { name: name, raw: raw }; } };`); err != nil {
		t.Fatal(err)
	}
	v, err := vm.RunString("(" + out + ")")
	if err != nil {
		t.Fatalf("evaluating %s: %v", out, err)
	}
	assertJS(t, vm, v, `$.at.name === 'vector' && $.at.raw[0] === 1.5 && $.at.raw[1] === -0.25 && $.label === 'origin'`)
}
