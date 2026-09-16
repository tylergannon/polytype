package structs

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"
)

// TestSchemaPropertiesMatchEncodingJSON uses encoding/json as the oracle for
// which properties a struct puts on the wire: the generated schema must name
// exactly the keys json.Marshal emits for a value of the root type.
func TestSchemaPropertiesMatchEncodingJSON(t *testing.T) {
	for name, tc := range map[string]struct {
		value  any
		schema json.RawMessage
	}{
		"JSONTagNames": {JSONTagNames{}, JSONTagNames{}.Schema()},
		"EmbeddedTags": {EmbeddedTags{IgnoredPointerBase: &IgnoredPointerBase{}}, EmbeddedTags{}.Schema()},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(tc.schema, &schema); err != nil {
				t.Fatal(err)
			}
			got := slices.Sorted(maps.Keys(schema.Properties))
			want := slices.Sorted(maps.Keys(wire))
			if !slices.Equal(got, want) {
				t.Fatalf("schema properties %v, encoding/json keys %v", got, want)
			}
		})
	}
}
