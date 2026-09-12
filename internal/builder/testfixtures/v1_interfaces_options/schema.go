//go:build jsonschema

package v1_interfaces_options

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage   { panic("not implemented") }
func (Owner) ValidateJSON([]byte) error { panic("not implemented") }

func (Plain) Schema() json.RawMessage   { panic("not implemented") }
func (Plain) ValidateJSON([]byte) error { panic("not implemented") }

var _ = polytype.NewJSONSchemaMethod(Plain.Schema)

// IFace is sealed by isIface, so Impl1 and Impl2 are inferred for every
// Owner field of that type.
var _ = polytype.NewJSONSchemaMethod(Owner.Schema)

var _ = polytype.SealedUnion[IFace]("!kind")
