//go:build jsonschema

package basictypes

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (EnumType) Schema() json.RawMessage {
	panic("not implemented")
}

func (EnumType) ValidateJSON([]byte) error { panic("not implemented") }

func (SliceOfEnumType) Schema() json.RawMessage {
	panic("not implemented")
}

func (SliceOfEnumType) ValidateJSON([]byte) error { panic("not implemented") }

func (SliceOfRemoteEnumType) Schema() json.RawMessage {
	panic("not implemented")
}

func (SliceOfRemoteEnumType) ValidateJSON([]byte) error { panic("not implemented") }

func (SliceOfPointerToRemoteEnum) Schema() json.RawMessage {
	panic("not implemented")
}

func (SliceOfPointerToRemoteEnum) ValidateJSON([]byte) error { panic("not implemented") }

// EnumHolder is registered so the generated schema's enum set and the
// generated enum codec are proven against the same document.
func (EnumHolder) Schema() json.RawMessage {
	panic("not implemented")
}

func (EnumHolder) ValidateJSON([]byte) error { panic("not implemented") }

var (
	_ = polytype.NewJSONSchemaMethod(EnumType.Schema)
	_ = polytype.NewJSONSchemaMethod(SliceOfEnumType.Schema)
	_ = polytype.NewJSONSchemaMethod(SliceOfRemoteEnumType.Schema)
	_ = polytype.NewJSONSchemaMethod(SliceOfPointerToRemoteEnum.Schema)
	_ = polytype.NewJSONSchemaMethod(EnumHolder.Schema)
)
