package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalSchemaHardlines(t *testing.T) {
	t.Run("compact leaves and arrays", func(t *testing.T) {
		schema := ArrayNode{
			Desc: "Values.",
			Items: PropertyNode[string]{
				Typ:      "string",
				Nullable: true,
				Desc:     "Value.",
			},
		}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"type":"array","description":"Values.","items":{"type":["string","null"],"description":"Value."}}`, string(actual))
		require.True(t, json.Valid(actual))
	})

	t.Run("object properties and nested objects", func(t *testing.T) {
		schema := ObjectNode{
			Desc: "Settings.",
			Properties: ObjectPropSet{
				{
					Name: "timeout",
					Schema: PropertyNode[string]{
						Typ:      "string",
						Nullable: true,
					},
				},
				{
					Name: "human",
					Schema: ObjectNode{
						Desc: "Human settings.",
						Properties: ObjectPropSet{
							{
								Name:   "enabled",
								Schema: PropertyNode[bool]{Typ: "boolean"},
							},
						},
					},
				},
			},
		}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"type":"object",
"description":"Settings.","properties":{
"timeout":{"type":["string","null"]},
"human":{"type":"object",
"description":"Human settings.","properties":{
"enabled":{"type":"boolean"}
},"required":["enabled"],"additionalProperties":false}
},"required":["timeout","human"],"additionalProperties":false}`, string(actual))
		require.True(t, json.Valid(actual))
	})

	t.Run("object unions", func(t *testing.T) {
		schema := UnionTypeNode{
			Options: []ObjectNode{
				{
					Discriminator: "Circle",
					Properties: ObjectPropSet{
						{Name: "radius", Schema: PropertyNode[float64]{Typ: "number"}},
					},
				},
				{
					Discriminator: "Square",
					Properties: ObjectPropSet{
						{Name: "width", Schema: PropertyNode[float64]{Typ: "number"}},
					},
				},
			},
		}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"anyOf":[{"type":"object",
"properties":{
"type":{"type":"string","const":"Circle"},
"radius":{"type":"number"}
},"required":["type","radius"],"additionalProperties":false},
{"type":"object",
"properties":{
"type":{"type":"string","const":"Square"},
"width":{"type":"number"}
},"required":["type","width"],"additionalProperties":false}]}`, string(actual))
		require.True(t, json.Valid(actual))
	})

	t.Run("nullable objects", func(t *testing.T) {
		schema := NullableObjectNode{Object: ObjectNode{
			Properties: ObjectPropSet{
				{Name: "value", Schema: PropertyNode[string]{Typ: "string"}},
			},
		}}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"anyOf":[{"type":"object",
"properties":{
"value":{"type":"string"}
},"required":["value"],"additionalProperties":false},{"type":"null"}]}`, string(actual))
		require.True(t, json.Valid(actual))
	})

	t.Run("root definitions", func(t *testing.T) {
		schema := RootSchema{
			Root: ObjectNode{},
			Defs: map[string]JSONSchema{
				"B": ObjectNode{},
				"A": PropertyNode[string]{Typ: "string"},
			},
		}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"$defs":{
"A":{"type":"string"},
"B":{"type":"object",
"additionalProperties":false}
},"type":"object",
"additionalProperties":false}`, string(actual))
		require.True(t, json.Valid(actual))
	})

	t.Run("refs and template holes stay compact", func(t *testing.T) {
		schema := ObjectNode{
			Properties: ObjectPropSet{
				{Name: "address", Schema: RefNode{Ref: "#/$defs/Address"}},
				{Name: "dynamic", Schema: TemplateHoleNode{Name: "dynamic"}},
			},
		}

		actual, err := MarshalHardlines(schema)
		require.NoError(t, err)
		require.Equal(t, `{"type":"object",
"properties":{
"address":{"$ref":"#/$defs/Address"},
"dynamic":{{.dynamic}}
},"required":["address","dynamic"],"additionalProperties":false}`, string(actual))
	})
}
