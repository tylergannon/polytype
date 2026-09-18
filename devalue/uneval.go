package devalue

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// A Replacer is devalue's `uneval` replacer hook: a chance to emit custom
// JavaScript for a value before the built-in type handling sees it.
//
// It is called once for each non-primitive value, on first encounter. When it
// returns ok, the trusted JavaScript string it returns is spliced into the
// output verbatim and the value's contents are not walked. Replacer is not a
// flat-format [Reducer]. `uneval` is a nested emitter for
// producing the expression of a sub-value — SvelteKit's transport uses it to
// write `app.decode("name", <uneval(encoded)>)`.
//
// The nested emitter runs in its own namespace, exactly as devalue's does, so
// references it emits are not shared with the enclosing document.
type Replacer func(v any, uneval func(any) (string, error)) (string, bool, error)

// DevalueError is the Go form of devalue's DevalueError: a value devalue
// refuses to serialize, with the path at which it was found.
type DevalueError struct {
	Message string
	Path    string
}

func (e *DevalueError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Message + " (at " + e.Path + ")"
}

// Uneval turns a value into the JavaScript expression that creates an
// equivalent value — devalue's `uneval`, which is what SvelteKit's SSR
// document embeds for hydration.
//
// Values referenced more than once (and every cycle) are hoisted into an
// immediately-invoked function so that reference identity survives the round
// trip: `(function(a){a.self=a;return a}({}))`.
func Uneval(v any) (string, error) {
	return UnevalWith(v, nil)
}

// QuoteString writes s as the JavaScript string literal devalue writes: a JSON
// string in which `<` becomes \u003C, so that nothing a value carries can close
// the <script> element the document puts it in.
//
// It is [Uneval] of a string, exposed on its own because a document is also
// full of strings that are not values — a module URL, a cache key — and those
// have to be escaped by the same rule.
func QuoteString(s string) string { return quoteString(s) }

// UnevalWith is [Uneval] with a replacer hook.
func UnevalWith(v any, replacer Replacer) (string, error) {
	u := &unevaler{replacer: replacer, byKey: map[any]*refEntry{}}

	if err := u.walk(v); err != nil {
		return "", err
	}

	// Values seen more than once become hoisted parameters, most-referenced
	// first. The sort must be stable: devalue sorts a Map's entries, which are
	// in first-encounter order, and the resulting parameter order is visible
	// in the output.
	var named []*refEntry
	for _, e := range u.order {
		if e.count > 1 {
			named = append(named, e)
		}
	}
	sort.SliceStable(named, func(i, j int) bool { return named[i].count > named[j].count })
	for i, e := range named {
		e.name = getName(i)
	}

	str, err := u.stringify(v)
	if err != nil {
		return "", err
	}
	if len(named) == 0 {
		return str, nil
	}

	var params, values, statements, reconstructions []string
	for _, e := range named {
		params = append(params, e.name)
		val, recon, stmts, err := u.hoist(e)
		if err != nil {
			return "", err
		}
		values = append(values, val)
		if recon != "" {
			reconstructions = append(reconstructions, recon)
		}
		statements = append(statements, stmts...)
	}
	statements = append(statements, "return "+str)

	body := strings.Join(append(reconstructions, statements...), ";")

	// A function may have at most 65535 parameters. Past that, pass the
	// hoisted values as one array argument and destructure them.
	if len(params) > 65534 {
		return "(function(){var[" + strings.Join(params, ",") + "]=arguments[0];" + body + "}([" + strings.Join(values, ",") + "]))", nil
	}
	return "(function(" + strings.Join(params, ",") + "){" + body + "}(" + strings.Join(values, ",") + "))", nil
}

// refEntry is one non-primitive value seen during the walk.
type refEntry struct {
	value     any
	count     int
	name      string
	custom    string
	hasCustom bool
}

type unevaler struct {
	replacer Replacer
	byKey    map[any]*refEntry
	order    []*refEntry
	path     []string
}

// sub is the nested emitter handed to the replacer.
func (u *unevaler) sub(v any) (string, error) { return UnevalWith(v, u.replacer) }

func (u *unevaler) errorf(msg string) error {
	return &DevalueError{Message: msg, Path: strings.Join(u.path, "")}
}

// walk counts references and runs the replacer, mirroring devalue's `walk`.
func (u *unevaler) walk(v any) error {
	if bigint, ok := v.(BigInt); ok && !validBigInt(bigint) {
		return u.errorf("Cannot stringify arbitrary non-POJOs")
	}
	if isPrimitive(v) {
		return nil
	}
	if isHole(v) {
		return u.errorf("a hole can only appear inside an array")
	}

	key, dedupe := refKey(v)
	if !dedupe {
		// An empty array, buffer or map: no identity to track, no children to
		// walk, and it cannot take part in a cycle. See refKey.
		if isEmptyContainer(v) {
			return nil
		}
		// Anything else without an identity — a typed nil, an uncomparable
		// value — still gets one chance at the replacer before it is refused.
		if u.replacer != nil {
			_, ok, err := u.replacer(v, u.sub)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
		return u.errorf("Cannot stringify arbitrary non-POJOs")
	}
	if e, ok := u.byKey[key]; ok {
		e.count++
		return nil
	}

	e := &refEntry{value: v, count: 1}
	u.byKey[key] = e
	u.order = append(u.order, e)

	if u.replacer != nil {
		s, ok, err := u.replacer(v, u.sub)
		if err != nil {
			return err
		}
		if ok {
			e.custom = s
			e.hasCustom = true
			return nil
		}
	}

	switch t := v.(type) {
	case RegExp:
		if !validRegExpFlags(t.Flags) {
			return u.errorf("Cannot stringify arbitrary non-POJOs")
		}
		return nil

	case Temporal:
		if !validTemporalKind(t.Kind) {
			return u.errorf("Cannot stringify arbitrary non-POJOs")
		}
		return nil

	case Date, URL, URLSearchParams, ArrayBuffer, *Boxed:
		// Leaves: devalue serializes these whole and never descends.
		return nil

	case *TypedArray:
		if t.Kind.BytesPerElement() == 0 {
			return u.errorf("Cannot stringify arbitrary non-POJOs")
		}
		return u.walk(t.Buffer)

	case *DataView:
		return u.walk(t.Buffer)

	case []any:
		for i, item := range t {
			if isHole(item) {
				// Array#forEach skips holes, so devalue never walks one.
				continue
			}
			u.path = append(u.path, "["+strconv.Itoa(i)+"]")
			if err := u.walk(item); err != nil {
				return err
			}
			u.path = u.path[:len(u.path)-1]
		}
		return nil

	case *Set:
		for _, item := range t.Items() {
			if err := u.walk(item); err != nil {
				return err
			}
		}
		return nil

	case *Map:
		for _, entry := range t.Entries() {
			desc := "..."
			if isPrimitive(entry.Key) {
				desc = stringifyPrimitive(entry.Key)
			}
			u.path = append(u.path, ".get("+desc+")")
			if err := u.walk(entry.Key); err != nil {
				return err
			}
			if err := u.walk(entry.Value); err != nil {
				return err
			}
			u.path = u.path[:len(u.path)-1]
		}
		return nil

	case *Object:
		for _, k := range propertyOrder(t.Keys()) {
			val, _ := t.Get(k)
			if err := u.walkProperty(k, val); err != nil {
				return err
			}
		}
		return nil

	case map[string]any:
		for _, k := range sortedKeys(t) {
			if err := u.walkProperty(k, t[k]); err != nil {
				return err
			}
		}
		return nil
	}

	return u.errorf("Cannot stringify arbitrary non-POJOs")
}

func (u *unevaler) walkProperty(k string, val any) error {
	if k == "__proto__" {
		return u.errorf("Cannot stringify objects with __proto__ keys")
	}
	if isIdentifier(k) {
		u.path = append(u.path, "."+k)
	} else {
		u.path = append(u.path, "["+quoteString(k)+"]")
	}
	if err := u.walk(val); err != nil {
		return err
	}
	u.path = u.path[:len(u.path)-1]
	return nil
}

// isEmptyContainer reports whether v is one of the container types refKey
// refuses to give an identity to because it is empty.
func isEmptyContainer(v any) bool {
	switch t := v.(type) {
	case []any:
		return len(t) == 0
	case ArrayBuffer:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// stringify renders one value, substituting a hoisted name where there is one.
func (u *unevaler) stringify(v any) (string, error) {
	if isPrimitive(v) {
		return stringifyPrimitive(v), nil
	}

	if key, dedupe := refKey(v); dedupe {
		if e := u.byKey[key]; e != nil {
			if e.name != "" {
				return e.name, nil
			}
			if e.hasCustom {
				return e.custom, nil
			}
		}
	} else if u.replacer != nil {
		// An empty container is not tracked, so the replacer is consulted here
		// instead of during the walk. It may therefore run more than once for
		// such a value; devalue runs it exactly once.
		s, ok, err := u.replacer(v, u.sub)
		if err != nil {
			return "", err
		}
		if ok {
			return s, nil
		}
	}

	return u.render(v)
}

// isNamed reports whether v was hoisted, which is devalue's `names.has(...)`.
func (u *unevaler) isNamed(v any) bool {
	key, dedupe := refKey(v)
	if !dedupe {
		return false
	}
	e := u.byKey[key]
	return e != nil && e.name != ""
}

// render emits a value inline, ignoring hoisting.
func (u *unevaler) render(v any) (string, error) {
	switch t := v.(type) {
	case *Boxed:
		inner, err := u.stringify(t.Value)
		if err != nil {
			return "", err
		}
		return "Object(" + inner + ")", nil

	case RegExp:
		return renderRegExp(t), nil

	case Date:
		return "new Date(" + formatNumber(float64(t.Time().UnixMilli())) + ")", nil

	case URL:
		return "new URL(" + quoteString(string(t)) + ")", nil

	case URLSearchParams:
		return "new URLSearchParams(" + quoteString(string(t)) + ")", nil

	case Temporal:
		return string(t.Kind) + ".from(" + quoteString(t.Value) + ")", nil

	case []any:
		return u.renderArray(t)

	case *Set:
		var parts []string
		for _, item := range t.Items() {
			s, err := u.stringify(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "new Set([" + strings.Join(parts, ",") + "])", nil

	case *Map:
		var parts []string
		for _, entry := range t.Entries() {
			// devalue stringifies each `[key, value]` pair as an array, which
			// is why there is no space after the comma here (unlike the
			// `.set(k, v)` form used for a hoisted Map).
			k, err := u.stringify(entry.Key)
			if err != nil {
				return "", err
			}
			val, err := u.stringify(entry.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, "["+k+","+val+"]")
		}
		return "new Map([" + strings.Join(parts, ",") + "])", nil

	case *TypedArray:
		return u.renderTypedArray(t)

	case *DataView:
		return u.renderDataView(t)

	case ArrayBuffer:
		return renderArrayBuffer(t), nil

	case *Object:
		return u.renderObject(propertyOrder(t.Keys()), t.NullProto, func(k string) any {
			val, _ := t.Get(k)
			return val
		})

	case map[string]any:
		return u.renderObject(sortedKeys(t), false, func(k string) any { return t[k] })
	}

	return "", u.errorf("Cannot stringify arbitrary non-POJOs")
}

func renderRegExp(t RegExp) string {
	if t.Flags == "" {
		return "new RegExp(" + quoteString(t.Source) + ")"
	}
	return "new RegExp(" + quoteString(t.Source) + `,"` + t.Flags + `")`
}

func renderArrayBuffer(buf ArrayBuffer) string {
	parts := make([]string, len(buf))
	for i, b := range buf {
		parts[i] = strconv.Itoa(int(b))
	}
	return "new Uint8Array([" + strings.Join(parts, ",") + "]).buffer"
}

// renderArray ports devalue's array case, holes and all.
//
// The choice between a holey array literal and `Object.assign(Array(n),{...})`
// is made at the first hole, and only then, so that a hugely sparse array is
// never iterated slot by slot.
func (u *unevaler) renderArray(items []any) (string, error) {
	n := len(items)
	hasHoles := false

	var b strings.Builder
	b.WriteByte('[')

	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		if !isHole(items[i]) {
			s, err := u.stringify(items[i])
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			continue
		}
		if hasHoles {
			// Already committed to the literal; a hole is just an empty slot.
			continue
		}

		// Array literal overhead is one comma per slot plus the brackets:
		// L + 2. Object.assign overhead is the 25-char wrapper plus the
		// length's digits, plus index + ":" + "," for each populated element:
		// (25 + d) + P * (d + 2).
		population := 0
		for _, item := range items {
			if !isHole(item) {
				population++
			}
		}
		d := len(strconv.Itoa(n))
		holeCost := n + 2
		sparseCost := 25 + d + population*(d+2)

		if holeCost > sparseCost {
			var sb strings.Builder
			sb.WriteString("Object.assign(Array(")
			sb.WriteString(strconv.Itoa(n))
			sb.WriteString("),{")
			first := true
			for j, item := range items {
				if isHole(item) {
					continue
				}
				if !first {
					sb.WriteByte(',')
				}
				first = false
				s, err := u.stringify(item)
				if err != nil {
					return "", err
				}
				sb.WriteString(strconv.Itoa(j))
				sb.WriteByte(':')
				sb.WriteString(s)
			}
			sb.WriteString("})")
			return sb.String(), nil
		}

		hasHoles = true
	}

	// A trailing hole needs an extra comma, because `[a,]` has length 1.
	tail := ""
	if n > 0 && isHole(items[n-1]) {
		tail = ","
	}
	return b.String() + tail + "]", nil
}

func (u *unevaler) renderTypedArray(t *TypedArray) (string, error) {
	var b strings.Builder
	b.WriteString("new ")
	b.WriteString(string(t.Kind))

	if !u.isNamed(t.Buffer) {
		elems, err := typedArrayElements(t.Kind, t.Buffer)
		if err != nil {
			return "", u.errorf(err.Error())
		}
		b.WriteString("([")
		b.WriteString(elems)
		b.WriteString("])")
	} else {
		s, err := u.stringify(t.Buffer)
		if err != nil {
			return "", err
		}
		b.WriteString("(")
		b.WriteString(s)
		b.WriteString(")")
	}

	if t.ByteLength != len(t.Buffer) {
		per := t.Kind.BytesPerElement()
		start := 0
		if per > 0 {
			start = t.ByteOffset / per
		}
		end := start + t.Len()
		b.WriteString(".subarray(")
		b.WriteString(strconv.Itoa(start))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(end))
		b.WriteByte(')')
	}

	return b.String(), nil
}

func (u *unevaler) renderDataView(t *DataView) (string, error) {
	var b strings.Builder
	b.WriteString("new DataView")

	if !u.isNamed(t.Buffer) {
		b.WriteString("(")
		b.WriteString(renderArrayBuffer(t.Buffer))
	} else {
		s, err := u.stringify(t.Buffer)
		if err != nil {
			return "", err
		}
		b.WriteString("(")
		b.WriteString(s)
	}

	if t.ByteLength != len(t.Buffer) {
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(t.ByteOffset))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(t.ByteLength))
	}

	b.WriteByte(')')
	return b.String(), nil
}

func (u *unevaler) renderObject(keys []string, nullProto bool, get func(string) any) (string, error) {
	var parts []string
	for _, k := range keys {
		if k == "__proto__" {
			return "", u.errorf("Cannot stringify objects with __proto__ keys")
		}
		s, err := u.stringify(get(k))
		if err != nil {
			return "", err
		}
		parts = append(parts, safeKey(k)+":"+s)
	}
	obj := strings.Join(parts, ",")
	if nullProto {
		if len(keys) > 0 {
			return "{" + obj + ",__proto__:null}", nil
		}
		return "{__proto__:null}", nil
	}
	return "{" + obj + "}", nil
}

// hoist returns the IIFE argument for a hoisted value, an optional
// reconstruction assignment, and the statements that fill it in.
//
// A reconstruction (`b=new Uint8Array(...)`) reassigns a `{}` placeholder and
// must run before the statements that reference it; devalue emits all of them
// first, which is safe because they depend only on the IIFE's arguments.
func (u *unevaler) hoist(e *refEntry) (value, reconstruction string, statements []string, err error) {
	if e.hasCustom {
		return e.custom, "", nil, nil
	}
	if isPrimitive(e.value) {
		return stringifyPrimitive(e.value), "", nil, nil
	}

	switch t := e.value.(type) {
	case *Boxed:
		inner, err := u.stringify(t.Value)
		if err != nil {
			return "", "", nil, err
		}
		return "Object(" + inner + ")", "", nil, nil

	case RegExp:
		return renderRegExp(t), "", nil, nil

	case Date:
		return "new Date(" + formatNumber(float64(t.Time().UnixMilli())) + ")", "", nil, nil

	case URL:
		return "new URL(" + quoteString(string(t)) + ")", "", nil, nil

	case URLSearchParams:
		return "new URLSearchParams(" + quoteString(string(t)) + ")", "", nil, nil

	case Temporal:
		return string(t.Kind) + ".from(" + quoteString(t.Value) + ")", "", nil, nil

	case []any:
		// Array#forEach skips holes, so a hoisted sparse array keeps them.
		for i, item := range t {
			if isHole(item) {
				continue
			}
			s, err := u.stringify(item)
			if err != nil {
				return "", "", nil, err
			}
			statements = append(statements, e.name+"["+strconv.Itoa(i)+"]="+s)
		}
		return "Array(" + strconv.Itoa(len(t)) + ")", "", statements, nil

	case *Set:
		var adds strings.Builder
		for _, item := range t.Items() {
			s, err := u.stringify(item)
			if err != nil {
				return "", "", nil, err
			}
			adds.WriteString(".add(" + s + ")")
		}
		// An empty Set is fully built by `new Set`; a chained statement would
		// otherwise be a dangling `name.`.
		if adds.Len() > 0 {
			statements = append(statements, e.name+adds.String())
		}
		return "new Set", "", statements, nil

	case *Map:
		var sets strings.Builder
		for _, entry := range t.Entries() {
			k, err := u.stringify(entry.Key)
			if err != nil {
				return "", "", nil, err
			}
			val, err := u.stringify(entry.Value)
			if err != nil {
				return "", "", nil, err
			}
			sets.WriteString(".set(" + k + ", " + val + ")")
		}
		if sets.Len() > 0 {
			statements = append(statements, e.name+sets.String())
		}
		return "new Map", "", statements, nil

	case *TypedArray:
		s, err := u.renderTypedArray(t)
		if err != nil {
			return "", "", nil, err
		}
		return "{}", e.name + "=" + s, nil, nil

	case *DataView:
		s, err := u.renderDataView(t)
		if err != nil {
			return "", "", nil, err
		}
		return "{}", e.name + "=" + s, nil, nil

	case ArrayBuffer:
		return renderArrayBuffer(t), "", nil, nil

	case *Object:
		stmts, err := u.hoistProperties(e.name, propertyOrder(t.Keys()), func(k string) any {
			val, _ := t.Get(k)
			return val
		})
		if err != nil {
			return "", "", nil, err
		}
		if t.NullProto {
			return "Object.create(null)", "", stmts, nil
		}
		return "{}", "", stmts, nil

	case map[string]any:
		stmts, err := u.hoistProperties(e.name, sortedKeys(t), func(k string) any { return t[k] })
		if err != nil {
			return "", "", nil, err
		}
		return "{}", "", stmts, nil
	}

	return "", "", nil, u.errorf("Cannot stringify arbitrary non-POJOs")
}

func (u *unevaler) hoistProperties(name string, keys []string, get func(string) any) ([]string, error) {
	var statements []string
	for _, k := range keys {
		if k == "__proto__" {
			return nil, u.errorf("Cannot stringify objects with __proto__ keys")
		}
		s, err := u.stringify(get(k))
		if err != nil {
			return nil, err
		}
		statements = append(statements, name+safeProp(k)+"="+s)
	}
	return statements, nil
}

// stringifyPrimitive is devalue's `stringify_primitive`.
func stringifyPrimitive(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case UndefinedValue:
		return "void 0"
	case string:
		return quoteString(t)
	case bool:
		return strconv.FormatBool(t)
	case BigInt:
		return string(t) + "n"
	}

	f, _ := asFloat(v)
	if f == 0 && math.Signbit(f) {
		return "-0"
	}
	s := formatNumber(f)
	// `String(0.1)` is "0.1"; devalue shortens it to ".1".
	if strings.HasPrefix(s, "0.") {
		return s[1:]
	}
	if strings.HasPrefix(s, "-0.") {
		return "-" + s[2:]
	}
	return s
}

// sortedKeys orders a Go map's keys by UTF-16 code unit, which is how
// JavaScript would have ordered them. A Go map has no property order, so a
// caller that owns one should pass an *Object instead.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	SortStringsUTF16(keys)
	return propertyOrder(keys)
}
