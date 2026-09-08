package polytype

import "encoding/json"

// This file is compile coverage for the Declare(...) fluent API: every
// declaration below only needs to compile to prove the signatures work for
// value receivers, pointer receivers, a free-function root, and joint
// field/provider type inference on Method/Function. None of it runs at
// runtime (same as every other marker in union_type.go).

type declareValueExample struct {
	A string
	B int
	C bool
}

func (declareValueExample) exampleSchema() json.RawMessage { panic("not implemented") }
func (declareValueExample) aSchema() json.Marshaler        { return nil }
func (declareValueExample) bSchema(int) json.Marshaler     { return nil }
func declareBoolSchema(bool) json.Marshaler                { return nil }

func declareFreeFuncRoot(declareValueExample) json.RawMessage { panic("not implemented") }

type declarePointerExample struct{ X string }

func (*declarePointerExample) exampleSchema() json.RawMessage { panic("not implemented") }
func (*declarePointerExample) xSchema() json.Marshaler        { return nil }

var (
	// Method-expression root, every chain method, provider type inference
	// for Accessor, Method, and Function from typed field references.
	_ = Declare(declareValueExample.exampleSchema).
		Accessor(Field[declareValueExample, string]("A"), declareValueExample.aSchema).
		Method(Field[declareValueExample, int]("B"), declareValueExample.bSchema).
		Function(Field[declareValueExample, bool]("C"), declareBoolSchema).
		StringerEnum(Field[declareValueExample, string]("A")).
		Ref().
		RenderProviders()

	// Free-function root: T is inferred from the function's sole parameter,
	// exactly as it is for a method expression.
	_ = Declare(declareFreeFuncRoot)

	// Pointer-receiver root and pointer-receiver Accessor provider.
	_ = Declare((*declarePointerExample).exampleSchema).
		Accessor(Field[declarePointerExample, string]("X"), (*declarePointerExample).xSchema)
)
