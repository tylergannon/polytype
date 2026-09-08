//go:build jsonschema

package model

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Envelope) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Envelope.Schema).StringerEnum(polytype.Field[Envelope, Priority]("Ranked"))

var _ = polytype.SealedUnion[Event]("kind")
