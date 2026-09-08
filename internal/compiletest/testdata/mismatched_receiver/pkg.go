package mismatched_receiver

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type A struct{ X string }
type B struct{ Y int }

func (A) Schema() json.RawMessage    { panic("x") }
func (B) YSchema(int) json.Marshaler { panic("x") }

var _ = polytype.Declare(A.Schema).Method(polytype.Field[A, string]("X"), B.YSchema)
