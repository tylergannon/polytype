//go:build jsonschema

package entrypoints

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (MethodType) Schema() json.RawMessage { panic("not implemented") }

func FuncTypeSchema(FuncType) json.RawMessage { panic("not implemented") }

func BuilderTypeSchema() json.RawMessage { panic("not implemented") }

func PointerFuncTypeSchema(PointerFuncType) json.RawMessage { panic("not implemented") }

func InterfaceFuncTypeSchema(InterfaceFuncType) json.RawMessage { panic("not implemented") }

var (
	_ = polytype.NewJSONSchemaMethod(MethodType.Schema)
	_ = polytype.NewJSONSchemaFunc[FuncType](FuncTypeSchema)
	_ = polytype.NewJSONSchemaBuilder[BuilderType](BuilderTypeSchema)
	_ = polytype.Declare(PointerFuncTypeSchema)
	_ = polytype.Declare(InterfaceFuncTypeSchema)
)
