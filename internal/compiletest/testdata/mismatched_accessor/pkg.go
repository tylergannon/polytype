package mismatched_accessor

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct{ A string }
type Other struct{ B string }

func (Example) Schema() json.RawMessage    { panic("x") }
func (Other) OtherASchema() json.Marshaler { panic("x") }

var _ = polytype.Declare(Example.Schema).Accessor(polytype.Field[Example, string]("A"), Other.OtherASchema)
