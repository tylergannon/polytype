package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/constant"
	"go/token"
	"strings"

	"github.com/tylergannon/polytype/typegrammar"
)

// maxNestingDepth bounds named-type nesting. Recursion is reported first, so
// only a genuinely deep chain of distinct definitions reaches it.
const maxNestingDepth = 100

// Root is one schema to render: a named definition, or a sealed interface
// rendered as the union of its variants (the grammar admits unions only in
// fields, so an interface root is passed as the union itself).
type Root struct {
	Name  typegrammar.Name
	Union *typegrammar.Union
}

// TypeName is the root's Go type name, as generated file names use it.
func (r Root) TypeName() string {
	if r.Union != nil {
		return r.Union.Interface.Name
	}
	return r.Name.Name
}

// Options selects per-definition rendering choices that are registrations,
// not type facts.
type Options struct {
	// Refs lists definitions rendered as "$ref" into "$defs" wherever
	// another definition references them. A root renders its own body, and
	// a union variant is always inlined.
	Refs map[typegrammar.Name]bool
}

// RecursionError reports a definition, or a union's interface, that reaches
// itself. JSON Schema inlines every referenced type, so it cannot express
// the cycle.
type RecursionError struct {
	Root   Root
	Type   typegrammar.Name
	Source token.Position
}

func (e *RecursionError) Error() string {
	return fmt.Sprintf("JSON Schema cannot express the recursive type %s (%s), reached from root %s", e.Type, e.Source, e.Root.TypeName())
}

// Generate validates defs and renders one schema per root, in order. Every
// root shares one "$defs" namespace: two distinct definitions in Options.Refs
// with the same bare name are a collision. It never mutates defs.
func Generate(defs typegrammar.Definitions, roots []Root, opts Options) ([]JSONSchema, error) {
	if err := defs.Validate(); err != nil {
		return nil, fmt.Errorf("generate JSON Schema: %w", err)
	}
	p := &projector{
		defs:    make(map[typegrammar.Name]typegrammar.Definition, len(defs)),
		refs:    opts.Refs,
		refDefs: make(map[string]refDef),
	}
	for _, def := range defs {
		p.defs[def.Name] = def
	}
	out := make([]JSONSchema, 0, len(roots))
	for _, root := range roots {
		node, err := p.rootSchema(root)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

type refDef struct {
	name   typegrammar.Name
	schema JSONSchema
}

type projector struct {
	defs    map[typegrammar.Name]typegrammar.Definition
	refs    map[typegrammar.Name]bool
	refDefs map[string]refDef
	// active holds the definitions and union interfaces on the current
	// rendering path. A definition joins it when one of its fields follows a
	// reference, a union when it is entered; reaching an active name again
	// is recursion.
	active map[typegrammar.Name]bool
	depth  int
	// owner is the definition whose fields are being rendered.
	owner typegrammar.Name
	root  Root
}

func (p *projector) rootSchema(root Root) (JSONSchema, error) {
	p.root = root
	p.active = make(map[typegrammar.Name]bool)
	p.depth = 0
	p.owner = typegrammar.Name{}
	var (
		node JSONSchema
		err  error
	)
	if root.Union != nil {
		node, err = p.union(*root.Union)
	} else {
		node, err = p.definition(root.Name)
	}
	if err != nil {
		return nil, err
	}
	defs := make(map[string]JSONSchema)
	p.collectRefDefs(node, defs)
	if len(defs) > 0 {
		return RootSchema{Root: node, Defs: defs}, nil
	}
	return node, nil
}

// definition renders a named definition's body with its own description.
func (p *projector) definition(name typegrammar.Name) (JSONSchema, error) {
	def, ok := p.defs[name]
	if !ok {
		return nil, fmt.Errorf("generate JSON Schema: unresolved definition %s", name)
	}
	if p.active[name] {
		return nil, &RecursionError{Root: p.root, Type: name, Source: def.Source}
	}
	saved := p.owner
	p.owner = name
	defer func() { p.owner = saved }()
	return p.typ(def.Type, def.Description)
}

// ref renders a reference from a field of the current owner. The owner is on
// the path while the target renders, so a target that reaches the owner
// again is recursive. The referencing field's comment replaces the target's
// own description; a "$defs" reference carries none.
func (p *projector) ref(target typegrammar.Name, override string) (JSONSchema, error) {
	owner := p.owner
	if owner != (typegrammar.Name{}) && !p.active[owner] {
		p.active[owner] = true
		defer delete(p.active, owner)
	}
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxNestingDepth {
		return nil, fmt.Errorf("max nesting depth exceeded at %s", p.defs[owner].Source)
	}
	node, err := p.definition(target)
	if err != nil {
		return nil, err
	}
	if p.refs[target] {
		return p.registerRefDef(target, node)
	}
	if override != "" {
		if described, ok := node.(schemaNode); ok {
			return described.setDescription(override), nil
		}
	}
	return node, nil
}

// registerRefDef records (or reuses) a "$defs" entry for a definition in
// Options.Refs and returns the RefNode rendered in its place. A second,
// distinct definition wanting the same bare name is a hard error.
func (p *projector) registerRefDef(target typegrammar.Name, node JSONSchema) (JSONSchema, error) {
	name := target.Name
	ref := RefNode{Ref: "#/$defs/" + name}
	if existing, ok := p.refDefs[name]; ok {
		if existing.name != target {
			return nil, fmt.Errorf("AsRef definition name collision: %q is used by both %s and %s (registered at %s)", name, existing.name, target, p.defs[target].Source)
		}
		return ref, nil
	}
	p.refDefs[name] = refDef{name: target, schema: node}
	return ref, nil
}

// collectRefDefs gathers every "$defs" entry reachable from a rendered
// schema, transitively, keyed by bare definition name.
func (p *projector) collectRefDefs(schema JSONSchema, defs map[string]JSONSchema) {
	switch node := schema.(type) {
	case ObjectNode:
		for _, prop := range node.Properties {
			p.collectRefDefs(prop.Schema, defs)
		}
	case ArrayNode:
		if node.Items != nil {
			p.collectRefDefs(node.Items, defs)
		}
	case UnionTypeNode:
		for _, opt := range node.Options {
			p.collectRefDefs(opt, defs)
		}
	case NullableObjectNode:
		p.collectRefDefs(node.Object, defs)
	case NullableUnionNode:
		p.collectRefDefs(node.Schema, defs)
	case RefNode:
		name := strings.TrimPrefix(node.Ref, "#/$defs/")
		if _, ok := defs[name]; ok {
			return
		}
		def, ok := p.refDefs[name]
		if !ok {
			return
		}
		defs[name] = def.schema
		p.collectRefDefs(def.schema, defs)
	}
}

func (p *projector) typ(t typegrammar.Type, description string) (JSONSchema, error) {
	switch n := t.(type) {
	case *typegrammar.Scalar:
		return scalar(n.Kind, description), nil
	case *typegrammar.Time:
		return PropertyNode[string]{Desc: timeDescription(description), Typ: "string"}, nil
	case *typegrammar.Enum:
		return enumNode(n, description, true), nil
	case *typegrammar.Object:
		props, err := p.fields(n.Fields)
		if err != nil {
			return nil, err
		}
		return ObjectNode{Desc: description, Properties: props}, nil
	case *typegrammar.Pointer:
		return p.typ(n.Element, description)
	case *typegrammar.Slice:
		items, err := p.typ(n.Element, "")
		if err != nil {
			return nil, err
		}
		return ArrayNode{Desc: description, Items: items}, nil
	case *typegrammar.Array:
		items, err := p.typ(n.Element, "")
		if err != nil {
			return nil, err
		}
		return ArrayNode{Desc: description, Items: items}, nil
	case *typegrammar.Ref:
		return p.ref(n.Target, description)
	default:
		return nil, fmt.Errorf("generate JSON Schema: %s: unsupported type constructor %T", p.owner, t)
	}
}

func (p *projector) fields(fields []typegrammar.Field) (ObjectPropSet, error) {
	props := make(ObjectPropSet, 0, len(fields))
	for _, field := range fields {
		prop, err := p.field(field)
		if err != nil {
			return nil, err
		}
		props = append(props, prop)
	}
	return props, nil
}

func (p *projector) field(f typegrammar.Field) (ObjectProp, error) {
	prop := ObjectProp{Name: f.JSONName}
	var err error
	switch v := f.Value.(type) {
	case *typegrammar.Required:
		prop.Schema, err = p.fieldType(v.Type, f.Description)
	case *typegrammar.Optional:
		prop.Optional = true
		prop.Schema, err = p.fieldType(v.Type, f.Description)
	case *typegrammar.Nullable:
		var inner JSONSchema
		if inner, err = p.fieldType(v.Type, f.Description); err != nil {
			return ObjectProp{}, err
		}
		if prop.Schema, err = nullableSchema(inner); err != nil {
			return ObjectProp{}, fmt.Errorf("polytype.Nullable field %s at %s: %w", f.JSONName, f.Source, err)
		}
	case *typegrammar.Union:
		prop.Schema, err = p.union(*v)
	case *typegrammar.OptionalUnion:
		prop.Optional = true
		prop.Schema, err = p.union(v.Union)
	case *typegrammar.UnionSlice:
		var union UnionTypeNode
		if union, err = p.union(v.Union); err != nil {
			return ObjectProp{}, err
		}
		prop.Schema = ArrayNode{Desc: f.Description, Items: union}
	case *typegrammar.Provided:
		prop.Optional = v.Optional
		if v.Ref != "" {
			prop.Schema = RefNode{Ref: v.Ref}
		} else {
			prop.Schema = TemplateHoleNode{Name: f.JSONName}
		}
	default:
		return ObjectProp{}, fmt.Errorf("generate JSON Schema: %s.%s: unsupported field constructor %T", p.owner, f.GoName, f.Value)
	}
	if err != nil {
		return ObjectProp{}, err
	}
	return prop, nil
}

// fieldType renders a direct field's type. An inline enum is a field-local
// registration: it renders its wire values and no description, unlike a
// reference to a reusable enum definition.
func (p *projector) fieldType(t typegrammar.Type, description string) (JSONSchema, error) {
	if enum, ok := t.(*typegrammar.Enum); ok {
		return enumNode(enum, "", false), nil
	}
	return p.typ(t, description)
}

func (p *projector) union(u typegrammar.Union) (UnionTypeNode, error) {
	if p.active[u.Interface] {
		return UnionTypeNode{}, &RecursionError{Root: p.root, Type: u.Interface, Source: u.Source}
	}
	p.active[u.Interface] = true
	defer delete(p.active, u.Interface)
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxNestingDepth {
		return UnionTypeNode{}, fmt.Errorf("max nesting depth exceeded at %s", u.Source)
	}
	node := UnionTypeNode{DiscriminatorPropName: u.Discriminator}
	for _, variant := range u.Variants {
		rendered, err := p.definition(variant.Implementation)
		if err != nil {
			return UnionTypeNode{}, err
		}
		source := p.defs[variant.Implementation].Source
		object, ok := rendered.(ObjectNode)
		if !ok {
			return UnionTypeNode{}, fmt.Errorf("expected %s to be an object-type schema at %s", variant.Implementation.Name, source)
		}
		for _, property := range object.Properties {
			if property.Name == u.Discriminator {
				return UnionTypeNode{}, fmt.Errorf("variant %s of sealed interface %s has a payload property %q that collides with the discriminator property at %s", variant.Implementation.Name, u.Interface.Name, u.Discriminator, source)
			}
		}
		object.Discriminator = variant.Tag
		node.Options = append(node.Options, object)
	}
	return node, nil
}

func scalar(kind typegrammar.ScalarKind, description string) JSONSchema {
	switch kind {
	case typegrammar.String:
		return PropertyNode[string]{Desc: description, Typ: "string"}
	case typegrammar.Bool:
		return PropertyNode[bool]{Desc: description, Typ: "boolean"}
	case typegrammar.Float32, typegrammar.Float64:
		return PropertyNode[float64]{Desc: description, Typ: "number"}
	default:
		return PropertyNode[int]{Desc: description, Typ: "integer"}
	}
}

func timeDescription(description string) string {
	timeDesc := "RFC3339 formatted date-time string (e.g., \"2006-01-02T15:04:05Z07:00\")"
	if description != "" {
		return description + ". Must be an " + timeDesc
	}
	return timeDesc
}

// enumNode renders an enum. withDescriptions folds member doc comments into
// the description, keyed by wire value.
func enumNode(enum *typegrammar.Enum, description string, withDescriptions bool) JSONSchema {
	if enum.Kind == typegrammar.String {
		values := make([]string, 0, len(enum.Members))
		for _, member := range enum.Members {
			values = append(values, constant.StringVal(member.Value))
		}
		return PropertyNode[string]{Typ: "string", Enum: values, Desc: enumDescription(description, enum.Members, values, withDescriptions)}
	}
	if enum.Mode == typegrammar.EnumNames {
		values := make([]string, 0, len(enum.Members))
		for _, member := range enum.Members {
			values = append(values, member.Name)
		}
		return PropertyNode[string]{Typ: "string", Enum: values, Desc: enumDescription(description, enum.Members, values, withDescriptions)}
	}
	values := make([]json.Number, 0, len(enum.Members))
	labels := make([]string, 0, len(enum.Members))
	for _, member := range enum.Members {
		exact := member.Value.ExactString()
		values = append(values, json.Number(exact))
		labels = append(labels, exact)
	}
	return PropertyNode[json.Number]{Typ: "integer", Enum: values, Desc: enumDescription(description, enum.Members, labels, withDescriptions)}
}

func enumDescription(base string, members []typegrammar.EnumMember, wireValues []string, enabled bool) string {
	if !enabled {
		return ""
	}
	var comments strings.Builder
	for i, member := range members {
		if member.Description == "" {
			continue
		}
		if comments.Len() > 0 {
			comments.WriteString("\n\n")
		}
		comments.WriteString(wireValues[i])
		comments.WriteString(": \n")
		comments.WriteString(member.Description)
	}
	if base != "" && comments.Len() > 0 {
		return base + "\n\n" + comments.String()
	}
	if base != "" {
		return base
	}
	return comments.String()
}

func nullableSchema(schema JSONSchema) (JSONSchema, error) {
	switch value := schema.(type) {
	case PropertyNode[int]:
		return nullableProperty(value)
	case PropertyNode[json.Number]:
		return nullableProperty(value)
	case PropertyNode[string]:
		return nullableProperty(value)
	case PropertyNode[bool]:
		return nullableProperty(value)
	case PropertyNode[float64]:
		return nullableProperty(value)
	case ObjectNode:
		return NullableObjectNode{Object: value}, nil
	case RefNode:
		return NullableUnionNode{Schema: value}, nil
	default:
		return nil, fmt.Errorf("inner schema shape %T is unsupported; supported nullable values are scalars, enums, structs, pointers to structs, and AsRef structs", schema)
	}
}

func nullableProperty[T ~int | ~string | ~bool | float32 | float64](value PropertyNode[T]) (JSONSchema, error) {
	if value.Const != nil {
		return nil, errors.New("consts are unsupported; supported nullable values are scalars, enums, structs, pointers to structs, and AsRef structs")
	}
	if len(value.Enum) > 0 {
		return NullableUnionNode{Schema: value}, nil
	}
	value.Nullable = true
	return value, nil
}
