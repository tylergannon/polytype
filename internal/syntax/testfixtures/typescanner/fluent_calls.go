//go:build jsonschema

package typescanner

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
	alias "github.com/tylergannon/polytype/internal/syntax/testfixtures/typescanner/scannersubpkg"
)

type FluentStruct struct {
	A string
	B int
	C bool
	E NiceEnumType
	F NiceEnumType
	G MarkerInterface
}

func (FluentStruct) Schema() json.RawMessage        { panic("not implemented") }
func (FluentStruct) ASchema() json.Marshaler        { panic("not implemented") }
func (FluentStruct) BSchema(int) json.Marshaler     { panic("not implemented") }
func FluentBoolSchema(bool) json.Marshaler          { panic("not implemented") }
func FluentFreeSchema(FluentStruct) json.RawMessage { panic("not implemented") }

func (t *FluentStruct) PtrSchema() json.RawMessage    { panic("not implemented") }
func (t *FluentStruct) PtrASchema() json.Marshaler    { panic("not implemented") }
func (t *FluentStruct) PtrBSchema(int) json.Marshaler { panic("not implemented") }

// One fluent chain exercising every supported chain method.
var _ = polytype.Declare(FluentStruct.Schema).
	Accessor(polytype.Field[FluentStruct, string]("A"), FluentStruct.ASchema).
	Method(polytype.Field[FluentStruct, int]("B"), FluentStruct.BSchema).
	Function(polytype.Field[FluentStruct, bool]("C"), FluentBoolSchema).
	StringerEnum(polytype.Field[FluentStruct, NiceEnumType]("F")).
	Ref().
	RenderProviders()

// A pointer-root fluent chain, proving providerRef recognizes the
// (*FluentStruct).Method form alongside the value-receiver form above.
var _ = polytype.Declare((*FluentStruct).PtrSchema).
	Accessor(polytype.Field[FluentStruct, string]("A"), (*FluentStruct).PtrASchema).
	Method(polytype.Field[FluentStruct, int]("B"), (*FluentStruct).PtrBSchema)

// A free-function root, with no chained options.
var _ = polytype.Declare(FluentFreeSchema)

// A bare Declare with no chain at all.
var _ = polytype.Declare(FluentStruct.Schema)

// The same alias-imported package used elsewhere in this fixture, proving
// import aliases resolve identically through the fluent chain-walk (which
// reuses the same IdentifyFunc/GetPackageForPrefix machinery).
var _ = polytype.Declare(alias.TypeForSchemaMethod.Schema).
	Ref()
