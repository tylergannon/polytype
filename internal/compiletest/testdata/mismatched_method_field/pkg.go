package mismatched_method_field

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Example struct {
	A string
	B int
}

func (Example) Schema() json.RawMessage { panic("x") }
func (Example) BSchema(int) json.Marshaler {
	panic("x")
}

// Field A is a string, but BSchema takes an int: the field and provider
// must jointly infer the same F, so this must fail to compile.
var _ = polytype.Declare(Example.Schema).Method(polytype.Field[Example, string]("A"), Example.BSchema)
