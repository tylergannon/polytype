package codegen

import (
	"fmt"
	"go/constant"
	"strconv"

	"github.com/tylergannon/polytype/typegrammar"
)

// encode emits the statements that convert the Go expression src into the
// devalue value model, and returns the name of the resulting any-typed
// variable. at is a Go expression holding the value's diagnostic path.
func (e *emitter) encode(t typegrammar.Type, src, at string) (string, error) {
	out := e.name("enc")
	switch n := t.(type) {
	case *typegrammar.Scalar:
		switch n.Kind {
		case typegrammar.Bool:
			e.writef("var %s any = bool(%s)", out, src)
		case typegrammar.String:
			e.writef("var %s any = string(%s)", out, src)
		default:
			// Every Go numeric kind is a JavaScript number, converted here
			// rather than by the runtime's own numeric handling.
			e.writef("var %s any = float64(%s)", out, src)
		}
		return out, nil

	case *typegrammar.Time:
		e.writef("%s, err := dvEncodeTime(time.Time(%s), %s)", out, src, at)
		e.check()
		return out, nil

	case *typegrammar.Enum:
		return e.encodeEnum(n, src, at, out)

	case *typegrammar.Object:
		e.writef("%s := devalue.NewObject()", out)
		for _, field := range n.Fields {
			if err := e.encodeField(field, out, src, at); err != nil {
				return "", err
			}
		}
		return out, nil

	case *typegrammar.Pointer:
		e.writef("if %s == nil {", src)
		e.fail(at, "nil pointer where a value is required")
		e.writef("}")
		return e.encode(n.Element, "(*"+src+")", at)

	case *typegrammar.Slice:
		// A nil required slice encodes as an empty array, never as null.
		e.writef("%s := make([]any, 0, len(%s))", out, src)
		if err := e.encodeElements(n.Element, out, src, at); err != nil {
			return "", err
		}
		boxed := e.name("enc")
		e.writef("var %s any = %s", boxed, out)
		return boxed, nil

	case *typegrammar.Array:
		e.writef("%s := make([]any, 0, %d)", out, n.Length)
		if err := e.encodeElements(n.Element, out, src, at); err != nil {
			return "", err
		}
		boxed := e.name("enc")
		e.writef("var %s any = %s", boxed, out)
		return boxed, nil

	case *typegrammar.Ref:
		e.writef("%s, err := enc%s(%s, %s)", out, e.g.bases[n.Target], src, at)
		e.check()
		return out, nil

	default:
		return "", e.errorf("unsupported type constructor %T", t)
	}
}

// encodeElements appends every element of the Go collection src to the any
// slice named dst, indexing the diagnostic path by position.
func (e *emitter) encodeElements(element typegrammar.Type, dst, src, at string) error {
	index, item := e.name("i"), e.name("item")
	sub := e.branch()
	encoded, err := sub.encode(element, item, sub.indexPath(at, index))
	if err != nil {
		return err
	}
	sub.writef("%s = append(%s, %s)", dst, dst, encoded)
	e.loop(sub, index, item, src)
	return nil
}

func (e *emitter) encodeEnum(n *typegrammar.Enum, src, at, out string) (string, error) {
	e.writef("var %s any", out)
	e.writef("switch {")
	emitted := make(map[string]bool, len(n.Members))
	for _, member := range n.Members {
		wire := e.g.enumWireLiteral(n, member)
		if emitted[wire] {
			// An alias: a later member with a value an earlier one already
			// carries. Its case would compare identically and never be
			// reached, so the earlier member's case stands for both.
			continue
		}
		emitted[wire] = true
		operand, err := e.g.enumGoLiteral(n, member)
		if err != nil {
			return "", e.errorf("enum member %s: %v", member.Name, err)
		}
		e.writef("case %s == %s:", src, operand)
		e.writef("%s = %s", out, wire)
	}
	e.writef("default:")
	e.fail(at, "value %v is not a member of enum "+n.GoType.String(), src)
	e.writef("}")
	return out, nil
}

func (e *emitter) encodeField(field typegrammar.Field, object, src, at string) error {
	value := src + "." + field.GoName
	key := strconv.Quote(field.JSONName)
	switch n := field.Value.(type) {
	case *typegrammar.Required:
		fieldPath := e.childPath(at, field.JSONName)
		encoded, err := e.encode(n.Type, value, fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Set(%s, %s)", object, key, encoded)
		return nil

	case *typegrammar.Optional:
		// An absent Optional is no property at all.
		e.writef("if %s.Present {", value)
		fieldPath := e.childPath(at, field.JSONName)
		encoded, err := e.encode(n.Type, value+".Value", fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Set(%s, %s)", object, key, encoded)
		e.writef("}")
		return nil

	case *typegrammar.Nullable:
		e.writef("if %s.Present {", value)
		fieldPath := e.childPath(at, field.JSONName)
		encoded, err := e.encode(n.Type, value+".Value", fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Set(%s, %s)", object, key, encoded)
		e.writef("} else {")
		e.writef("%s.Set(%s, nil)", object, key)
		e.writef("}")
		return nil

	case *typegrammar.Union:
		fieldPath := e.childPath(at, field.JSONName)
		encoded, err := e.encodeUnion(*n, value, fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Set(%s, %s)", object, key, encoded)
		return nil

	case *typegrammar.OptionalUnion:
		e.writef("if %s.Present {", value)
		fieldPath := e.childPath(at, field.JSONName)
		encoded, err := e.encodeUnion(n.Union, value+".Value", fieldPath)
		if err != nil {
			return err
		}
		e.writef("%s.Set(%s, %s)", object, key, encoded)
		e.writef("}")
		return nil

	case *typegrammar.UnionSlice:
		fieldPath := e.childPath(at, field.JSONName)
		items := e.name("enc")
		e.writef("%s := make([]any, 0, len(%s))", items, value)
		index, item := e.name("i"), e.name("item")
		e.writef("for %s, %s := range %s {", index, item, value)
		itemPath := e.indexPath(fieldPath, index)
		encoded, err := e.encodeUnion(n.Union, item, itemPath)
		if err != nil {
			return err
		}
		e.writef("%s = append(%s, %s)", items, items, encoded)
		e.writef("}")
		e.writef("%s.Set(%s, %s)", object, key, items)
		return nil

	default:
		return e.errorf("field %s: unsupported field constructor %T", field.GoName, field.Value)
	}
}

// encodeUnion emits one object carrying the discriminator and the concrete
// variant's properties. The discriminator is written first and any value the
// variant itself carried under that key is replaced by the resolved tag.
func (e *emitter) encodeUnion(u typegrammar.Union, src, at string) (string, error) {
	payload, tag := e.name("payload"), e.name("tag")
	e.writef("var %s any", payload)
	e.writef("var %s string", tag)
	bound := e.name("variant")
	e.writef("switch %s := %s.(type) {", bound, src)
	for _, variant := range u.Variants {
		base, ok := e.g.bases[variant.Implementation]
		if !ok {
			return "", e.errorf("unresolved union implementation %s", variant.Implementation)
		}
		goType := e.g.qualify(variant.Implementation)
		operand := bound
		if variant.Pointer {
			goType = "*" + goType
			operand = "(*" + bound + ")"
		}
		e.writef("case %s:", goType)
		if variant.Pointer {
			e.writef("if %s == nil {", bound)
			e.fail(at, "nil pointer where a value is required")
			e.writef("}")
		}
		e.writef("%s = %s", tag, strconv.Quote(variant.Tag))
		encoded := e.name("enc")
		e.writef("%s, err := enc%s(%s, %s)", encoded, base, operand, at)
		e.check()
		e.writef("%s = %s", payload, encoded)
	}
	e.writef("default:")
	e.fail(at, "value of type %T is not a variant of union "+u.Interface.String(), bound)
	e.writef("}")

	out := e.name("enc")
	e.writef("%s, err := dvTagged(%s, %s, %s, %s)", out, payload, strconv.Quote(u.Discriminator), tag, at)
	e.check()
	return out, nil
}

// enumGoLiteral renders the Go constant a member compares equal to. Comparing
// against the value, not the constant's identifier, keeps the emitted code
// working for unexported constants.
func (g *generator) enumGoLiteral(n *typegrammar.Enum, member typegrammar.EnumMember) (string, error) {
	goType := g.qualify(n.GoType)
	if n.Kind == typegrammar.String {
		return fmt.Sprintf("%s(%s)", goType, strconv.Quote(constant.StringVal(member.Value))), nil
	}
	return fmt.Sprintf("%s(%s)", goType, member.Value.ExactString()), nil
}

// enumWireLiteral renders the value a member takes on the wire, honoring Mode.
// Every numeric kind is a JavaScript number, so a member beyond 2^53 rounds to
// the nearest representable one exactly as a scalar field of the same kind
// does. The conversion is not an error; it is the decided wire mapping.
func (g *generator) enumWireLiteral(n *typegrammar.Enum, member typegrammar.EnumMember) string {
	if n.Mode == typegrammar.EnumNames {
		return strconv.Quote(member.Name)
	}
	if n.Kind == typegrammar.String {
		return strconv.Quote(constant.StringVal(member.Value))
	}
	return fmt.Sprintf("float64(%s)", member.Value.ExactString())
}
