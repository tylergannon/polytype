//go:build jsonschema

package crosspkg_union

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Named) Schema() json.RawMessage    { panic("not implemented") }
func (Embedded) Schema() json.RawMessage { panic("not implemented") }
func (Many) Schema() json.RawMessage     { panic("not implemented") }

var (
	_ = polytype.Declare(Named.Schema)
	_ = polytype.Declare(Embedded.Schema)
	_ = polytype.Declare(Many.Schema)
)
