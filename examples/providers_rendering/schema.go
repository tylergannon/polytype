//go:build jsonschema

package providers_rendering

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Example) Schema() json.RawMessage { panic("not implemented") }

// v1: RenderProviders() generates RenderedSchema() that executes providers.
var _ = polytype.Declare(Example.Schema).
	Accessor(polytype.Field[Example, string]("A"), (Example).ASchema).
	Method(polytype.Field[Example, int]("B"), (Example).BSchema).
	Function(polytype.Field[Example, bool]("C"), BoolSchema).
	RenderProviders()
