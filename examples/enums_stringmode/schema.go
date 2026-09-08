//go:build jsonschema

package enums_stringmode

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Paint) Schema() json.RawMessage { panic("not implemented") }

// Enum string mode example: Color is a numeric enum, but .StringerEnum
// renders it as a JSON string enum of its declared constant names.
var _ = polytype.Declare(Paint.Schema).
	StringerEnum(polytype.Field[Paint, Color]("C"))
