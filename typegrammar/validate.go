package typegrammar

import (
	"fmt"
	"go/constant"
	"go/token"
	"reflect"
	"unicode/utf8"
)

// Error identifies the first invalid constructor in deterministic definition,
// field, and variant order. Source is the nearest available source location;
// Path is usable even for programmatically constructed definitions without one.
type Error struct {
	Path    string
	Source  token.Position
	Message string
}

func (e *Error) Error() string {
	if e.Source.IsValid() {
		return fmt.Sprintf("%s: %s: %s", e.Source, e.Path, e.Message)
	}
	return e.Path + ": " + e.Message
}

// Validate admits exactly the constructors and compositions documented by this
// package. It checks all definitions, including unused ones. It neither mutates
// nor normalizes the graph, executes user code, nor validates runtime JSON.
//
// The graph's edges are child types, references, object field operands and
// union implementations. Rejecting every back edge establishes a finite DAG;
// each constructor can therefore be projected by structural induction. Sharing
// is permitted and checked once per relevant context, rather than mistaken for
// recursion. Source-level admissibility (including aliases, tags, interface
// satisfaction, custom hooks and supported package discovery) remains lowering's
// obligation because those facts are not recoverable from a resolved graph.
func (defs Definitions) Validate() error {
	v := validator{
		defs:   make(map[Name]Definition, len(defs)),
		active: make(map[Type]string),
		done:   make(map[visit]bool),
	}
	for i, def := range defs {
		at := location{path: fmt.Sprintf("definitions[%d]", i), source: def.Source}
		if !validName(def.Name) {
			return at.fail("invalid resolved type name %q", def.Name)
		}
		if _, exists := v.defs[def.Name]; exists {
			return at.fail("duplicate definition %s", def.Name)
		}
		v.defs[def.Name] = def
	}
	for _, def := range defs {
		if err := v.walk(def.Type, location{path: def.Name.String(), source: def.Source}, true, true); err != nil {
			return err
		}
	}
	return nil
}

type location struct {
	path   string
	source token.Position
}

func (at location) child(path string, source token.Position) location {
	if !source.IsValid() {
		source = at.source
	}
	return location{path: at.path + path, source: source}
}

func (at location) fail(format string, args ...any) error {
	return &Error{Path: at.path, Source: at.source, Message: fmt.Sprintf(format, args...)}
}

type visit struct {
	node       Type
	namedOwner bool
	directEnum bool
}

type validator struct {
	defs   map[Name]Definition
	active map[Type]string
	done   map[visit]bool
}

func validName(n Name) bool {
	return n.PackagePath != "" && utf8.ValidString(n.PackagePath) && token.IsIdentifier(n.Name)
}

// The interface is sealed, but embedding an exported node can promote its
// marker method. Check the exact constructor before using it as a map key;
// unknown implementations (including noncomparable embedded values) are errors.
func knownType(t Type) bool {
	switch t.(type) {
	case *Scalar, *Time, *Enum, *Object, *Pointer, *Slice, *Array, *Ref:
		return !reflect.ValueOf(t).IsNil()
	default:
		return false
	}
}

func (v *validator) walk(t Type, at location, namedOwner, directEnum bool) error {
	if !knownType(t) {
		return at.fail("nil or unsupported type constructor %T", t)
	}
	if from, exists := v.active[t]; exists {
		return at.fail("cyclic type: back edge to %s", from)
	}
	key := visit{t, namedOwner, directEnum}
	if v.done[key] {
		return nil
	}
	v.active[t] = at.path
	defer delete(v.active, t)
	var err error
	switch n := t.(type) {
	case *Scalar:
		if !validScalar(n.Kind) {
			err = at.fail("unsupported scalar kind %q", n.Kind)
		}
	case *Time:
	case *Enum:
		if n.Mode == EnumNames && !directEnum {
			err = at.fail("string-mode enum adaptation requires a direct field of a named object owner")
		} else {
			err = validateEnum(n, at)
		}
	case *Object:
		err = v.object(n, at, namedOwner)
	case *Pointer:
		err = v.walk(n.Element, at.child(".pointer", token.Position{}), false, false)
	case *Slice:
		err = v.walk(n.Element, at.child(".items", token.Position{}), false, false)
		if err == nil && v.byteLike(n.Element) {
			err = at.fail("byte-like slices have a base64 wire mapping outside this grammar")
		}
	case *Array:
		if n.Length < 0 {
			err = at.fail("array length must be nonnegative")
		} else {
			err = v.walk(n.Element, at.child(".items", token.Position{}), false, false)
		}
	case *Ref:
		def, exists := v.defs[n.Target]
		if !exists {
			err = at.fail("unresolved reference %s", n.Target)
		} else {
			err = v.walk(def.Type, at.child(" -> "+n.Target.String(), def.Source), true, directEnum)
		}
	}
	if err == nil {
		v.done[key] = true
	}
	return err
}

func (v *validator) object(n *Object, at location, namedOwner bool) error {
	goNames := make(map[string]bool, len(n.Fields))
	jsonNames := make(map[string]bool, len(n.Fields))
	for _, field := range n.Fields {
		fieldAt := at.child("."+field.GoName, field.Source)
		if !token.IsIdentifier(field.GoName) || field.GoName == "_" {
			return fieldAt.fail("invalid resolved Go field name %q", field.GoName)
		}
		if goNames[field.GoName] {
			return fieldAt.fail("duplicate Go field name %q", field.GoName)
		}
		if !utf8.ValidString(field.JSONName) {
			return fieldAt.fail("JSON property name is not valid UTF-8")
		}
		if jsonNames[field.JSONName] {
			return fieldAt.fail("duplicate JSON property name %q", field.JSONName)
		}
		goNames[field.GoName], jsonNames[field.JSONName] = true, true
		if err := v.field(field.Value, fieldAt, namedOwner); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) field(f FieldValue, at location, namedOwner bool) error {
	switch n := f.(type) {
	case *Required:
		if n != nil {
			return v.walk(n.Type, at, false, namedOwner)
		}
	case *Optional:
		if n != nil {
			return v.walk(n.Type, at, false, namedOwner)
		}
	case *Nullable:
		if n != nil {
			if err := v.walk(n.Type, at, false, namedOwner); err != nil {
				return err
			}
			if !v.nullable(n.Type) {
				return at.fail("unsupported Nullable operand: expected scalar, enum, object, time.Time, or pointer to object")
			}
			return nil
		}
	case *Union:
		if n != nil {
			return v.union(n, at, namedOwner)
		}
	case *OptionalUnion:
		if n != nil {
			return v.union(&n.Union, at, namedOwner)
		}
	case *UnionSlice:
		if n != nil {
			return v.union(&n.Union, at, namedOwner)
		}
	}
	return at.fail("nil or unsupported field constructor %T", f)
}

// These helpers run only after walk has established resolved, acyclic operands.
func (v *validator) dereference(t Type) Type {
	for {
		r, ok := t.(*Ref)
		if !ok {
			return t
		}
		t = v.defs[r.Target].Type
	}
}

func (v *validator) byteLike(t Type) bool {
	switch n := v.dereference(t).(type) {
	case *Scalar:
		return n.Kind == Uint8
	case *Enum:
		return n.Kind == Uint8
	default:
		return false
	}
}

func (v *validator) nullable(t Type) bool {
	switch n := v.dereference(t).(type) {
	case *Scalar, *Enum, *Object, *Time:
		return true
	case *Pointer:
		_, ok := v.dereference(n.Element).(*Object)
		return ok
	default:
		return false
	}
}

func (v *validator) union(n *Union, at location, namedOwner bool) error {
	if !namedOwner {
		return at.fail("registered unions require a direct field of a named object owner")
	}
	if !validName(n.Interface) {
		return at.fail("invalid resolved interface name %q", n.Interface)
	}
	if n.Discriminator == "" || !utf8.ValidString(n.Discriminator) {
		return at.fail("resolved discriminator must be a nonempty UTF-8 property name")
	}
	if len(n.Variants) == 0 {
		return at.fail("registered union must have at least one variant")
	}
	tags := make(map[string]bool, len(n.Variants))
	type implementation struct {
		name    Name
		pointer bool
	}
	implementations := make(map[implementation]bool, len(n.Variants))
	for i, variant := range n.Variants {
		variantAt := at.child(fmt.Sprintf(".variants[%d]", i), variant.Source)
		if !utf8.ValidString(variant.Tag) {
			return variantAt.fail("discriminator value is not valid UTF-8")
		}
		if tags[variant.Tag] {
			return variantAt.fail("duplicate discriminator value %q", variant.Tag)
		}
		id := implementation{variant.Implementation, variant.Pointer}
		if implementations[id] {
			return variantAt.fail("duplicate implementation %s (pointer=%t)", variant.Implementation, variant.Pointer)
		}
		tags[variant.Tag], implementations[id] = true, true
		def, exists := v.defs[variant.Implementation]
		if !exists {
			return variantAt.fail("unresolved implementation %s", variant.Implementation)
		}
		if err := v.walk(def.Type, variantAt, true, true); err != nil {
			return err
		}
		payload, ok := v.dereference(def.Type).(*Object)
		if !ok {
			return variantAt.fail("union implementation must resolve to an object")
		}
		for _, field := range payload.Fields {
			if field.JSONName == n.Discriminator && !v.admitsTag(field.Value, variant.Tag) {
				return variantAt.fail("payload property %q conflicts with discriminator value %q", n.Discriminator, variant.Tag)
			}
		}
	}
	return nil
}

// A preexisting payload property is narrowed to a required literal in this
// union occurrence; the shared payload definition is never modified. This is a
// structural constraint, not proof that a custom marshaler emits a matching tag.
func (v *validator) admitsTag(f FieldValue, tag string) bool {
	var t Type
	switch n := f.(type) {
	case *Required:
		t = n.Type
	case *Optional:
		t = n.Type
	case *Nullable:
		t = n.Type
	default:
		return false
	}
	for {
		t = v.dereference(t)
		switch n := t.(type) {
		case *Pointer:
			t = n.Element
		case *Scalar:
			return n.Kind == String
		case *Time:
			return true // The structural wire type is string; runtime validity is separate.
		case *Enum:
			for _, m := range n.Members {
				if n.Mode == EnumNames && m.Name == tag || n.Kind == String && constant.StringVal(m.Value) == tag {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

func validScalar(k ScalarKind) bool {
	return k == Bool || k == String || k == Float32 || k == Float64 || integerBits(k) != 0
}

func integerBits(k ScalarKind) int {
	switch k {
	case Int8, Uint8:
		return 8
	case Int16, Uint16:
		return 16
	case Int32, Uint32:
		return 32
	case Int, Int64, Uint, Uint64:
		return 64
	default:
		return 0
	}
}

func validateEnum(n *Enum, at location) error {
	if !validName(n.GoType) {
		return at.fail("invalid enum Go type %q", n.GoType)
	}
	if n.Kind != String && integerBits(n.Kind) == 0 {
		return at.fail("enum underlying kind must be string or integer")
	}
	if n.Mode != EnumValues && n.Mode != EnumNames || n.Mode == EnumNames && n.Kind == String {
		return at.fail("invalid enum mode for underlying kind %s", n.Kind)
	}
	if len(n.Members) == 0 {
		return at.fail("enum must have at least one member")
	}
	names := make(map[string]bool, len(n.Members))
	values := make(map[string]bool, len(n.Members))
	for i, member := range n.Members {
		memberAt := at.child(fmt.Sprintf(".members[%d]", i), token.Position{})
		if !token.IsIdentifier(member.Name) || member.Name == "_" || names[member.Name] {
			return memberAt.fail("invalid or duplicate enum constant name %q", member.Name)
		}
		names[member.Name] = true
		if member.Value == nil {
			return memberAt.fail("nil enum constant")
		}
		if n.Kind == String {
			if member.Value.Kind() != constant.String || !utf8.ValidString(constant.StringVal(member.Value)) {
				return memberAt.fail("expected UTF-8 string constant")
			}
		} else if !integerFits(member.Value, n.Kind) {
			return memberAt.fail("enum constant must be an exact integer within %s range", n.Kind)
		}
		value := member.Value.ExactString()
		if n.Mode == EnumNames && values[value] {
			return memberAt.fail("ambiguous string-mode enum: multiple names for value %s", value)
		}
		values[value] = true
	}
	return nil
}

// Int/Uint validate against maximum Go width here. The source adapter must
// additionally check platform-sized constants with the target package's sizes.
func integerFits(value constant.Value, kind ScalarKind) bool {
	if value.Kind() != constant.Int {
		return false
	}
	bits := integerBits(kind)
	switch kind {
	case Uint, Uint8, Uint16, Uint32, Uint64:
		n, ok := constant.Uint64Val(value)
		return ok && (bits == 64 || n < uint64(1)<<bits)
	default:
		n, ok := constant.Int64Val(value)
		return ok && (bits == 64 || n >= -(int64(1)<<(bits-1)) && n < int64(1)<<(bits-1))
	}
}
