//go:build jsonschema

package model

import (
	"encoding/json"
	"github.com/tylergannon/polytype"
)

func (Todo) Schema() json.RawMessage { panic("stub") }
func (Todo) ValidateJSON([]byte) error { panic("stub") }
var _ = polytype.Declare(Todo.Schema)
