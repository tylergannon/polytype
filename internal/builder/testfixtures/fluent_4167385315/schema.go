//go:build jsonschema

package fixture

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct {
	A string `json:"a"`
	B int    `json:"b"`
}

func (*Example) Schema() json.RawMessage { panic("not implemented") }
func (*Example) ASchema() json.Marshaler {
	return json.RawMessage(`{"type":"string","description":"A"}`)
}
func (*Example) BSchema(_ int) json.Marshaler {
	return json.RawMessage(`{"type":"integer","description":"B"}`)
}

var _ = polytype.Declare((*Example).Schema).
	Accessor(polytype.Field[Example, string]("A"), (*Example).ASchema).
	Method(polytype.Field[Example, int]("B"), (*Example).BSchema)
