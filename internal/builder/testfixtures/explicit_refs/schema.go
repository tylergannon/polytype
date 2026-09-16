//go:build jsonschema

package explicit_refs

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var (
	_ = polytype.Declare(Owner.Schema).
		StringerEnum(polytype.Field[Owner, Level]("L"))
	_ = polytype.Declare[Linked]()
)
