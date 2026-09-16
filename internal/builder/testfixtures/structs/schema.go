//go:build jsonschema

package structs

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (StructType1) Schema() json.RawMessage {
	panic("not implemented")
}

func (StructType2) Schema() json.RawMessage {
	panic("not implemented")
}

func (StructWithRefs) Schema() json.RawMessage {
	panic("not implemented")
}

func (JSONTagNames) Schema() json.RawMessage {
	panic("not implemented")
}

func (EmbeddedTags) Schema() json.RawMessage {
	panic("not implemented")
}

var (
	_ = polytype.NewJSONSchemaMethod(StructType1.Schema)
	_ = polytype.NewJSONSchemaMethod(StructType2.Schema)
	_ = polytype.NewJSONSchemaMethod(StructWithRefs.Schema)
	_ = polytype.NewJSONSchemaMethod(JSONTagNames.Schema)
	_ = polytype.Declare(EmbeddedTags.Schema)
)
