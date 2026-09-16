package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MarshalHardlines renders compact JSON with line breaks owned by schema
// structure rather than output width. Leaf schemas remain compact. Object
// schemas break after their type marker and put each property on its own
// line.
func MarshalHardlines(schema json.Marshaler) ([]byte, error) {
	var sb strings.Builder
	if err := writeSchemaHardlines(&sb, schema); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func writeSchemaHardlines(sb *strings.Builder, schema json.Marshaler) error {
	switch node := schema.(type) {
	case RootSchema:
		return writeRootSchemaHardlines(sb, node)
	case *RootSchema:
		return writeRootSchemaHardlines(sb, *node)
	case ObjectNode:
		return writeObjectHardlines(sb, node)
	case *ObjectNode:
		return writeObjectHardlines(sb, *node)
	case NullableObjectNode:
		return writeNullableObjectHardlines(sb, node)
	case *NullableObjectNode:
		return writeNullableObjectHardlines(sb, *node)
	case NullableUnionNode:
		return writeNullableUnionHardlines(sb, node)
	case *NullableUnionNode:
		return writeNullableUnionHardlines(sb, *node)
	case ArrayNode:
		return writeArrayHardlines(sb, node)
	case *ArrayNode:
		return writeArrayHardlines(sb, *node)
	case UnionTypeNode:
		return writeUnionHardlines(sb, node)
	case *UnionTypeNode:
		return writeUnionHardlines(sb, *node)
	default:
		data, err := schema.MarshalJSON()
		if err != nil {
			return err
		}
		_, _ = sb.Write(data)
		return nil
	}
}

func writeObjectHardlines(sb *strings.Builder, object ObjectNode) error {
	sb.WriteString(`{"type":"object",`)
	sb.WriteByte('\n')

	needsComma := false
	if object.Desc != "" {
		sb.WriteString(`"description":`)
		encodeString(sb, object.Desc)
		needsComma = true
	}

	if len(object.Properties) > 0 {
		if needsComma {
			sb.WriteByte(',')
		}
		sb.WriteString(`"properties":{`)
		sb.WriteByte('\n')
		for i, property := range object.Properties {
			encodeString(sb, property.Name)
			sb.WriteByte(':')
			if err := writeSchemaHardlines(sb, property.Schema); err != nil {
				return fmt.Errorf("object property %q: %w", property.Name, err)
			}
			if i < len(object.Properties)-1 {
				sb.WriteByte(',')
			}
			sb.WriteByte('\n')
		}
		sb.WriteByte('}')
		needsComma = true
	}

	required := requiredPropertyNames(object.Properties)
	if len(required) > 0 {
		if needsComma {
			sb.WriteByte(',')
		}
		sb.WriteString(`"required":[`)
		for i, name := range required {
			if i > 0 {
				sb.WriteByte(',')
			}
			encodeString(sb, name)
		}
		sb.WriteByte(']')
		needsComma = true
	}

	if needsComma {
		sb.WriteByte(',')
	}
	sb.WriteString(`"additionalProperties":false}`)
	return nil
}

func writeArrayHardlines(sb *strings.Builder, array ArrayNode) error {
	sb.WriteString(`{"type":"array"`)
	if array.Desc != "" {
		sb.WriteString(`,"description":`)
		encodeString(sb, array.Desc)
	}
	if array.Items != nil {
		sb.WriteString(`,"items":`)
		if err := writeSchemaHardlines(sb, array.Items); err != nil {
			return fmt.Errorf("array items: %w", err)
		}
	}
	sb.WriteByte('}')
	return nil
}

func writeNullableObjectHardlines(sb *strings.Builder, nullable NullableObjectNode) error {
	sb.WriteString(`{"anyOf":[`)
	if err := writeObjectHardlines(sb, nullable.Object); err != nil {
		return err
	}
	sb.WriteString(`,{"type":"null"}]}`)
	return nil
}

func writeNullableUnionHardlines(sb *strings.Builder, nullable NullableUnionNode) error {
	sb.WriteString(`{"anyOf":[`)
	if err := writeSchemaHardlines(sb, nullable.Schema); err != nil {
		return err
	}
	sb.WriteString(`,{"type":"null"}]}`)
	return nil
}

func writeUnionHardlines(sb *strings.Builder, union UnionTypeNode) error {
	sb.WriteString(`{"anyOf":[`)
	for i, option := range union.Options {
		if i > 0 {
			sb.WriteString(",\n")
		}
		if err := writeObjectHardlines(sb, prependDiscriminator(option, union.DiscriminatorPropName)); err != nil {
			return fmt.Errorf("union option %d: %w", i, err)
		}
	}
	sb.WriteString(`]}`)
	return nil
}

func writeRootSchemaHardlines(sb *strings.Builder, root RootSchema) error {
	var rootJSON strings.Builder
	if err := writeSchemaHardlines(&rootJSON, root.Root); err != nil {
		return err
	}
	rootText := rootJSON.String()
	if len(rootText) < 2 || rootText[0] != '{' {
		return fmt.Errorf("RootSchema: root schema must marshal to a JSON object")
	}

	names := make([]string, 0, len(root.Defs))
	for name := range root.Defs {
		names = append(names, name)
	}
	sort.Strings(names)

	sb.WriteString(`{"$defs":{`)
	if len(names) > 0 {
		sb.WriteByte('\n')
	}
	for i, name := range names {
		encodeString(sb, name)
		sb.WriteByte(':')
		if err := writeSchemaHardlines(sb, root.Defs[name]); err != nil {
			return fmt.Errorf("$defs %q: %w", name, err)
		}
		if i < len(names)-1 {
			sb.WriteByte(',')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(`},`)
	sb.WriteString(rootText[1:])
	return nil
}
