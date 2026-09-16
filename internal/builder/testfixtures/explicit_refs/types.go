// Package explicit_refs covers fields whose JSON Schema is an explicit ref
// while their Go type is a sealed union or a string-mode enum: the ref wins in
// the schema, and the generated codecs still adapt the Go value.
package explicit_refs

// Shape is sealed by its unexported method.
type Shape interface{ shape() }

// Circle is a round shape.
type Circle struct {
	Radius int `json:"radius"`
}

func (Circle) shape() {}

// Level is an integer enum rendered by constant name.
type Level int

func (Level) enum() {}

const (
	Low Level = iota
	High
)

// Holder has a union field. Roots reach it only through ref-tagged fields.
type Holder struct {
	S Shape `json:"s"`
}

// Owner's union and enum fields carry explicit refs.
type Owner struct {
	S Shape `json:"s" jsonschema:"ref=#/definitions/Shape"`
	L Level `json:"l" jsonschema:"ref=#/definitions/Level"`
}

// Linked is a codec-only root that reaches Holder only through a ref.
type Linked struct {
	H Holder `json:"h" jsonschema:"ref=#/definitions/Holder"`
}
