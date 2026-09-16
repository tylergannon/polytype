// Package schema projects the validated Go type grammar into JSON Schema.
//
// The input is a [github.com/tylergannon/polytype/typegrammar] definition
// graph plus the roots to render. The output is one schema node per root,
// which marshals to the compact or hardline JSON the CLI writes. The node
// types in this file are the projection's output AST, not a second type
// model: they carry JSON Schema vocabulary only.
//
// JSON Schema inlines every type it references, so a recursive definition
// cannot be expressed; Generate reports it as a [RecursionError]. A
// definition listed in [Options.Refs] is rendered as a "$ref" into "$defs"
// wherever another definition references it.
package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const defaultDiscriminatorProperty = "type"

type (
	// JSONSchema is one rendered node.
	JSONSchema interface {
		json.Marshaler
		implementsJSONSchema()
	}

	// schemaNode is a node that carries a description a field comment can
	// replace: a referenced named type takes the referencing field's comment.
	schemaNode interface {
		Description() string
		setDescription(desc string) schemaNode
		JSONSchema
	}

	// ObjectProp is a single property in an ObjectNode.
	ObjectProp struct {
		Name     string
		Schema   JSONSchema
		Optional bool
	}

	ObjectPropSet []ObjectProp

	// ObjectNode is an object schema. Discriminator is the const written for
	// the discriminator property when the object is a union option.
	ObjectNode struct {
		Desc          string
		Properties    ObjectPropSet
		Discriminator string
	}

	// PropertyNode is a scalar schema (string, integer, number, boolean),
	// optionally constrained to an enum or a const.
	PropertyNode[T ~int | ~string | ~bool | float32 | float64] struct {
		Desc     string
		Enum     []T
		Const    *T
		Typ      string
		Nullable bool
	}

	// NullableObjectNode is an inlined object schema that also admits null.
	NullableObjectNode struct {
		Object ObjectNode
	}

	// NullableUnionNode wraps a schema whose constraints cannot be combined
	// with a nullable type array, such as an enum or a "$ref".
	NullableUnionNode struct {
		Schema JSONSchema
	}

	ArrayNode struct {
		Desc  string
		Items JSONSchema
	}

	// UnionTypeNode is {"anyOf": [<object with discriminator const>, ...]}.
	UnionTypeNode struct {
		DiscriminatorPropName string
		Options               []ObjectNode
	}

	RefNode struct {
		Ref string
	}

	// TemplateHoleNode writes a raw template placeholder like {{.field}} for
	// a property whose schema a runtime provider supplies.
	TemplateHoleNode struct {
		Name string
	}

	// RootSchema wraps a root schema with a "$defs" map, splicing "$defs" in
	// as the leading key so the rest of the root's key order is untouched.
	RootSchema struct {
		Root JSONSchema
		Defs map[string]JSONSchema
	}
)

func (r RefNode) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `{"$ref":"%s"}`, r.Ref), nil
}

func (r RefNode) implementsJSONSchema() {}

func (t TemplateHoleNode) MarshalJSON() ([]byte, error) {
	return []byte("{{." + t.Name + "}}"), nil
}

func (t TemplateHoleNode) implementsJSONSchema() {}

// MarshalJSON splices a "$defs" object in as the first key of the root
// schema's own marshaled output, preserving the root's existing key order.
func (r RootSchema) MarshalJSON() ([]byte, error) {
	rootBytes, err := r.Root.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if len(rootBytes) < 2 || rootBytes[0] != '{' {
		return nil, fmt.Errorf("RootSchema: root schema must marshal to a JSON object")
	}

	names := make([]string, 0, len(r.Defs))
	for name := range r.Defs {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(`{"$defs":{`)
	for i, name := range names {
		if i > 0 {
			sb.WriteByte(',')
		}
		encodeString(&sb, name)
		sb.WriteByte(':')
		data, err := r.Defs[name].MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("$defs %q: %w", name, err)
		}
		sb.Write(data)
	}
	sb.WriteString("},")
	sb.Write(rootBytes[1:])
	return []byte(sb.String()), nil
}

func (r RootSchema) implementsJSONSchema() {}

var (
	_ JSONSchema = UnionTypeNode{}
	_ schemaNode = ArrayNode{}
	_ schemaNode = PropertyNode[int]{}
	_ schemaNode = ObjectNode{}
	_ JSONSchema = RefNode{}
	_ JSONSchema = TemplateHoleNode{}
	_ JSONSchema = NullableObjectNode{}
	_ JSONSchema = NullableUnionNode{}
	_ JSONSchema = RootSchema{}
)

func (n NullableObjectNode) MarshalJSON() ([]byte, error) {
	object, err := n.Object.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `{"anyOf":[%s,{"type":"null"}]}`, object), nil
}

func (n NullableObjectNode) implementsJSONSchema() {}

func (n NullableUnionNode) MarshalJSON() ([]byte, error) {
	value, err := n.Schema.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `{"anyOf":[%s,{"type":"null"}]}`, value), nil
}

func (n NullableUnionNode) implementsJSONSchema() {}

func (o ObjectNode) Description() string { return o.Desc }

func (o ObjectNode) implementsJSONSchema() {}

func (o ObjectNode) setDescription(s string) schemaNode {
	o.Desc = s
	return o
}

// MarshalJSON for an ObjectNode does not write the discriminator property;
// UnionTypeNode prepends it to each option.
func (o ObjectNode) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	sb.WriteString(`"type":"object"`)
	if o.Desc != "" {
		sb.WriteString(`,"description":`)
		encodeString(&sb, o.Desc)
	}
	if len(o.Properties) > 0 {
		sb.WriteString(`,"properties":{`)
		for i, prop := range o.Properties {
			if i > 0 {
				sb.WriteByte(',')
			}
			encodeString(&sb, prop.Name)
			sb.WriteByte(':')
			data, err := prop.Schema.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("object property %q: %w", prop.Name, err)
			}
			sb.Write(data)
		}
		sb.WriteByte('}')
	}
	requiredFields := requiredPropertyNames(o.Properties)
	if len(requiredFields) > 0 {
		sb.WriteString(`,"required":[`)
		for i, rf := range requiredFields {
			if i > 0 {
				sb.WriteByte(',')
			}
			encodeString(&sb, rf)
		}
		sb.WriteByte(']')
	}
	sb.WriteString(`,"additionalProperties":false}`)
	return []byte(sb.String()), nil
}

func (p PropertyNode[T]) Description() string { return p.Desc }

func (p PropertyNode[T]) implementsJSONSchema() {}

func (p PropertyNode[T]) setDescription(s string) schemaNode {
	p.Desc = s
	return p
}

// MarshalJSON writes type, description, const, enum in that order.
func (p PropertyNode[T]) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	sb.WriteString(`"type":`)
	if p.Nullable {
		sb.WriteByte('[')
		encodeString(&sb, p.Typ)
		sb.WriteString(`,"null"]`)
	} else {
		encodeString(&sb, p.Typ)
	}
	if p.Desc != "" {
		sb.WriteString(`,"description":`)
		encodeString(&sb, p.Desc)
	}
	if constVal, isConst := toJSONValue(p.Const); isConst {
		sb.WriteString(`,"const":`)
		sb.WriteString(constVal)
	}
	if len(p.Enum) > 0 {
		sb.WriteString(`,"enum":[`)
		for i, val := range p.Enum {
			if i > 0 {
				sb.WriteByte(',')
			}
			strVal, _ := toJSONValue(&val)
			sb.WriteString(strVal)
		}
		sb.WriteByte(']')
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

// toJSONValue returns the JSON literal for a scalar value, and false for nil.
func toJSONValue[T ~int | ~string | ~bool | float64 | float32](v *T) (string, bool) {
	if v == nil {
		return "", false
	}
	switch u := any(*v).(type) {
	case string:
		b, _ := json.Marshal(u)
		return string(b), true
	case json.Number:
		return u.String(), true
	case bool:
		return strconv.FormatBool(u), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", u), true
	default:
		panic(fmt.Sprintf("unknown type to make into JSON value %T %#v", u, u))
	}
}

func (a ArrayNode) setDescription(s string) schemaNode {
	a.Desc = s
	return a
}

func (a ArrayNode) Description() string { return a.Desc }

func (a ArrayNode) implementsJSONSchema() {}

func (a ArrayNode) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	sb.WriteString(`"type":"array"`)
	if a.Desc != "" {
		sb.WriteString(`,"description":`)
		encodeString(&sb, a.Desc)
	}
	if a.Items != nil {
		sb.WriteString(`,"items":`)
		data, err := a.Items.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("arrayNode items: %w", err)
		}
		sb.Write(data)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

func (u UnionTypeNode) implementsJSONSchema() {}

// MarshalJSON writes {"anyOf": [<option with discriminator const>, ...]}.
func (u UnionTypeNode) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteString(`{"anyOf":[`)
	for i, obj := range u.Options {
		if i > 0 {
			sb.WriteByte(',')
		}
		data, err := prependDiscriminator(obj, u.DiscriminatorPropName).MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("union option %d: %w", i, err)
		}
		sb.Write(data)
	}
	sb.WriteString(`]}`)
	return []byte(sb.String()), nil
}

// prependDiscriminator returns o with a required const string property for
// the discriminator in front of its own properties.
func prependDiscriminator(o ObjectNode, discPropName string) ObjectNode {
	if discPropName == "" {
		discPropName = defaultDiscriminatorProperty
	}
	newProps := make(ObjectPropSet, len(o.Properties)+1)
	newProps[0] = ObjectProp{
		Name: discPropName,
		Schema: PropertyNode[string]{
			Typ:   "string",
			Const: &o.Discriminator,
		},
	}
	copy(newProps[1:], o.Properties)
	return ObjectNode{
		Desc:          o.Desc,
		Properties:    newProps,
		Discriminator: o.Discriminator,
	}
}

func requiredPropertyNames(properties ObjectPropSet) []string {
	required := make([]string, 0, len(properties))
	for _, property := range properties {
		if !property.Optional {
			required = append(required, property.Name)
		}
	}
	return required
}

func encodeString(sb *strings.Builder, s string) {
	b, _ := json.Marshal(s)
	sb.Write(b)
}
