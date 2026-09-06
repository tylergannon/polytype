package codegen

import (
	"strconv"
	"strings"

	"github.com/tylergannon/polytype/typegrammar"
)

// numericRange holds the wire bounds a Go integer kind admits. The bounds are
// float64 because every wire number is one.
var numericRange = map[typegrammar.ScalarKind][2]string{
	typegrammar.Int:    {"math.MinInt", "math.MaxInt"},
	typegrammar.Int8:   {"math.MinInt8", "math.MaxInt8"},
	typegrammar.Int16:  {"math.MinInt16", "math.MaxInt16"},
	typegrammar.Int32:  {"math.MinInt32", "math.MaxInt32"},
	typegrammar.Int64:  {"math.MinInt64", "math.MaxInt64"},
	typegrammar.Uint:   {"0", "math.MaxUint"},
	typegrammar.Uint8:  {"0", "math.MaxUint8"},
	typegrammar.Uint16: {"0", "math.MaxUint16"},
	typegrammar.Uint32: {"0", "math.MaxUint32"},
	typegrammar.Uint64: {"0", "math.MaxUint64"},
}

// decodeAs decodes into goType, which may be a named type whose underlying
// shape is node. An object is built in place; anything else is decoded
// structurally and converted.
func (e *emitter) decodeAs(node typegrammar.Type, goType, raw, at string) (string, error) {
	if object, ok := node.(*typegrammar.Object); ok {
		return e.decodeObject(object, goType, raw, at)
	}
	decoded, err := e.decode(node, raw, at)
	if err != nil {
		return "", err
	}
	structural, err := e.g.goType(node)
	if err != nil {
		return "", err
	}
	if goType == "" || goType == structural {
		return decoded, nil
	}
	out := e.name("dec")
	e.writef("%s := %s(%s)", out, goType, decoded)
	return out, nil
}

// decode emits the statements that convert the devalue value model expression
// raw into a Go value of the node's structural type, and returns its name.
func (e *emitter) decode(node typegrammar.Type, raw, at string) (string, error) {
	out := e.name("dec")
	switch n := node.(type) {
	case *typegrammar.Scalar:
		switch n.Kind {
		case typegrammar.Bool:
			e.writef("%s, err := dvBool(%s, %s)", out, raw, at)
			e.check()
		case typegrammar.String:
			e.writef("%s, err := dvString(%s, %s)", out, raw, at)
			e.check()
		case typegrammar.Float64:
			e.writef("%s, err := dvNumber(%s, %s)", out, raw, at)
			e.check()
		case typegrammar.Float32:
			e.writef("%s, err := dvFloat32(%s, %s)", out, raw, at)
			e.check()
		default:
			bounds, ok := numericRange[n.Kind]
			if !ok {
				return "", e.errorf("unsupported scalar kind %q", n.Kind)
			}
			number := e.name("num")
			e.writef("%s, err := dvInteger(%s, %s, %s, %s)", number, raw, at, bounds[0], bounds[1])
			e.check()
			e.writef("%s := %s(%s)", out, n.Kind, number)
		}
		return out, nil

	case *typegrammar.Time:
		e.writef("%s, err := dvDecodeTime(%s, %s)", out, raw, at)
		e.check()
		return out, nil

	case *typegrammar.Enum:
		return e.decodeEnum(n, raw, at, out)

	case *typegrammar.Object:
		goType, err := e.g.goType(n)
		if err != nil {
			return "", err
		}
		return e.decodeObject(n, goType, raw, at)

	case *typegrammar.Pointer:
		element, err := e.decode(n.Element, raw, at)
		if err != nil {
			return "", err
		}
		e.writef("%s := &%s", out, element)
		return out, nil

	case *typegrammar.Slice:
		elementType, err := e.g.goType(n.Element)
		if err != nil {
			return "", err
		}
		items := e.name("items")
		e.writef("%s, err := dvArray(%s, %s)", items, raw, at)
		e.check()
		e.writef("%s := make([]%s, 0, len(%s))", out, elementType, items)
		if err := e.decodeElements(n.Element, items, at, func(sub *emitter, decoded string) {
			sub.writef("%s = append(%s, %s)", out, out, decoded)
		}); err != nil {
			return "", err
		}
		return out, nil

	case *typegrammar.Array:
		elementType, err := e.g.goType(n.Element)
		if err != nil {
			return "", err
		}
		items := e.name("items")
		e.writef("%s, err := dvArray(%s, %s)", items, raw, at)
		e.check()
		e.writef("if len(%s) != %d {", items, n.Length)
		e.fail(at, "expected an array of "+strconv.FormatInt(n.Length, 10)+" elements, got %d", "len("+items+")")
		e.writef("}")
		e.writef("var %s [%d]%s", out, n.Length, elementType)
		index := ""
		if err := e.decodeElements(n.Element, items, at, func(sub *emitter, decoded string) {
			sub.writef("%s[%s] = %s", out, index, decoded)
		}, &index); err != nil {
			return "", err
		}
		return out, nil

	case *typegrammar.Ref:
		e.writef("%s, err := dec%s(%s, %s)", out, e.g.bases[n.Target], raw, at)
		e.check()
		return out, nil

	default:
		return "", e.errorf("unsupported type constructor %T", node)
	}
}

// decodeElements loops over items, decoding each element and handing the
// decoded variable's name to store. When indexOut is given it receives the
// loop index variable's name before store is called.
func (e *emitter) decodeElements(element typegrammar.Type, items, at string, store func(*emitter, string), indexOut ...*string) error {
	index, item := e.name("i"), e.name("item")
	for _, out := range indexOut {
		*out = index
	}
	sub := e.branch()
	decoded, err := sub.decode(element, item, sub.indexPath(at, index))
	if err != nil {
		return err
	}
	store(sub, decoded)
	e.loop(sub, index, item, items)
	return nil
}

func (e *emitter) decodeEnum(n *typegrammar.Enum, raw, at, out string) (string, error) {
	goType := e.g.qualify(n.GoType)
	wire := e.name("wire")
	if n.Mode == typegrammar.EnumNames || n.Kind == typegrammar.String {
		e.writef("%s, err := dvString(%s, %s)", wire, raw, at)
	} else {
		e.writef("%s, err := dvNumber(%s, %s)", wire, raw, at)
	}
	e.check()
	e.writef("var %s %s", out, goType)
	e.writef("switch {")
	for _, member := range n.Members {
		wireLiteral, err := e.g.enumWireLiteral(n, member)
		if err != nil {
			return "", e.errorf("enum member %s: %v", member.Name, err)
		}
		goLiteral, err := e.g.enumGoLiteral(n, member)
		if err != nil {
			return "", e.errorf("enum member %s: %v", member.Name, err)
		}
		e.writef("case %s == %s:", wire, wireLiteral)
		e.writef("%s = %s", out, goLiteral)
	}
	e.writef("default:")
	e.fail(at, "value %v is not a member of enum "+n.GoType.String(), wire)
	e.writef("}")
	return out, nil
}

func (e *emitter) decodeObject(n *typegrammar.Object, goType, raw, at string) (string, error) {
	out := e.name("dec")
	e.writef("var %s %s", out, goType)
	if err := e.decodeObjectInto(n, out, raw, at); err != nil {
		return "", err
	}
	return out, nil
}

// decodeObjectInto fills the Go value expression target from raw. It never
// spells the object's type, only selects fields off target, which is how an
// anonymous struct field is decoded: target is a selector on the parent value.
func (e *emitter) decodeObjectInto(n *typegrammar.Object, target, raw, at string) error {
	object := e.name("obj")
	e.writef("%s, err := dvObject(%s, %s)", object, raw, at)
	e.check()
	known := make([]string, 0, len(n.Fields))
	for _, field := range n.Fields {
		known = append(known, strconv.Quote(field.JSONName))
	}
	e.writef("if err := dvKnown(%s, %s%s); err != nil {", object, at, joinPrefixed(known))
	if e.zero == "" {
		e.writef("return nil, err")
	} else {
		e.writef("return %s, err", e.zero)
	}
	e.writef("}")

	for _, field := range n.Fields {
		if err := e.decodeField(field, object, target, at); err != nil {
			return err
		}
	}
	return nil
}

// assign emits the decoding of node into the Go value expression target. An
// anonymous object is filled in place through selectors on target because its
// type cannot be spelled; everything else decodes to a temporary first.
func (e *emitter) assign(node typegrammar.Type, target, raw, at string) error {
	if object, ok := node.(*typegrammar.Object); ok {
		return e.decodeObjectInto(object, target, raw, at)
	}
	decoded, err := e.decode(node, raw, at)
	if err != nil {
		return err
	}
	e.writef("%s = %s", target, decoded)
	return nil
}

func joinPrefixed(values []string) string {
	var out strings.Builder
	for _, value := range values {
		out.WriteString(", " + value)
	}
	return out.String()
}

func (e *emitter) decodeField(field typegrammar.Field, object, out, at string) error {
	target := out + "." + field.GoName
	key := strconv.Quote(field.JSONName)
	fieldPath := e.childPath(at, field.JSONName)

	required := func() string {
		value := e.name("raw")
		e.writef("%s, err := dvRequired(%s, %s, %s)", value, object, key, fieldPath)
		e.check()
		return value
	}
	// optional emits the presence lookup and opens the block that decodes a
	// present value. The caller closes it.
	optional := func() string {
		value, present := e.name("raw"), e.name("ok")
		e.writef("%s, %s, err := dvPresent(%s, %s, %s)", value, present, object, key, fieldPath)
		e.check()
		e.writef("if %s {", present)
		return value
	}

	switch n := field.Value.(type) {
	case *typegrammar.Required:
		return e.assign(n.Type, target, required(), fieldPath)

	case *typegrammar.Optional:
		value := optional()
		e.writef("%s.Present = true", target)
		if err := e.assign(n.Type, target+".Value", value, fieldPath); err != nil {
			return err
		}
		e.writef("}")
		return nil

	case *typegrammar.Nullable:
		value := required()
		// null is the absent case; anything else must decode as the operand.
		e.writef("if %s != nil {", value)
		e.writef("%s.Present = true", target)
		if err := e.assign(n.Type, target+".Value", value, fieldPath); err != nil {
			return err
		}
		e.writef("}")
		return nil

	case *typegrammar.Union:
		value := required()
		decoded, err := e.decodeUnion(*n, value, fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s = %s", target, decoded)
		return nil

	case *typegrammar.OptionalUnion:
		value := optional()
		decoded, err := e.decodeUnion(n.Union, value, fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Present = true", target)
		e.writef("%s.Value = %s", target, decoded)
		e.writef("}")
		return nil

	case *typegrammar.UnionSlice:
		value := required()
		items := e.name("items")
		e.writef("%s, err := dvArray(%s, %s)", items, value, fieldPath)
		e.check()
		e.writef("%s = make([]%s, 0, len(%s))", target, e.g.qualify(n.Union.Interface), items)
		index, item := e.name("i"), e.name("item")
		e.writef("for %s, %s := range %s {", index, item, items)
		itemPath := e.indexPath(fieldPath, index)
		decoded, err := e.decodeUnion(n.Union, item, itemPath)
		if err != nil {
			return err
		}
		e.writef("%s = append(%s, %s)", target, target, decoded)
		e.writef("}")
		return nil

	default:
		return e.errorf("field %s: unsupported field constructor %T", field.GoName, field.Value)
	}
}

// decodeUnion reads the discriminator, dispatches to the variant's decoder and
// returns the name of a variable of the interface type. A variant that does
// not declare the discriminator as a property never sees it.
func (e *emitter) decodeUnion(u typegrammar.Union, raw, at string) (string, error) {
	object := e.name("obj")
	e.writef("%s, err := dvObject(%s, %s)", object, raw, at)
	e.check()
	tagPath := e.childPath(at, u.Discriminator)
	tagRaw := e.name("raw")
	e.writef("%s, err := dvRequired(%s, %s, %s)", tagRaw, object, strconv.Quote(u.Discriminator), tagPath)
	e.check()
	tag := e.name("tag")
	e.writef("%s, err := dvString(%s, %s)", tag, tagRaw, tagPath)
	e.check()

	out := e.name("dec")
	e.writef("var %s %s", out, e.g.qualify(u.Interface))
	e.writef("switch %s {", tag)
	for _, variant := range u.Variants {
		base, ok := e.g.bases[variant.Implementation]
		if !ok {
			return "", e.errorf("unresolved union implementation %s", variant.Implementation)
		}
		e.writef("case %s:", strconv.Quote(variant.Tag))
		payload := object
		if !e.g.declaresProperty(variant.Implementation, u.Discriminator) {
			payload = e.name("payload")
			e.writef("%s := dvWithout(%s, %s)", payload, object, strconv.Quote(u.Discriminator))
		}
		decoded := e.name("dec")
		e.writef("%s, err := dec%s(%s, %s)", decoded, base, payload, at)
		e.check()
		if variant.Pointer {
			e.writef("%s = &%s", out, decoded)
		} else {
			e.writef("%s = %s", out, decoded)
		}
	}
	e.writef("default:")
	e.fail(tagPath, "%q is not a known "+u.Interface.String()+" variant", tag)
	e.writef("}")
	return out, nil
}

// declaresProperty reports whether the named definition is an object with a
// property of that JSON name.
func (g *generator) declaresProperty(name typegrammar.Name, jsonName string) bool {
	def, ok := g.index[name]
	if !ok {
		return false
	}
	object, ok := def.Type.(*typegrammar.Object)
	if !ok {
		return false
	}
	for _, field := range object.Fields {
		if field.JSONName == jsonName {
			return true
		}
	}
	return false
}
