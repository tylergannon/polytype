//go:build jsonschema

package crosspkg_shapes

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Holder) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Holder.Schema)
