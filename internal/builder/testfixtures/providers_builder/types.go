package providers_builder

import "encoding/json"

type Example struct {
	A string `json:"a"`
	B int    `json:"b"`
	C bool   `json:"c"`
}

// Provider implementations must be available in normal builds for RenderedSchema.
func (Example) ASchema() json.Marshaler {
	return json.RawMessage(`{"type":"string","description":"A"}`)
}
func (Example) BSchema(_ int) json.Marshaler {
	return json.RawMessage(`{"type":"integer","description":"B"}`)
}
func BoolSchemaFunc(_ bool) json.Marshaler {
	return json.RawMessage(`{"type":"boolean","description":"C"}`)
}
