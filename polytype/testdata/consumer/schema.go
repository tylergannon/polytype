//go:build jsonschema

package consumer

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Envelope) Schema() json.RawMessage    { panic("not implemented") }
func (Detail) Schema() json.RawMessage      { panic("not implemented") }
func (Composition) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Detail.Schema).Ref()
var _ = polytype.Declare(Composition.Schema)
var _ = polytype.Declare(Envelope.Schema).StringerEnum(Envelope{}.PriorityName)
var _ = polytype.SealedUnion[Event]("!kind")
