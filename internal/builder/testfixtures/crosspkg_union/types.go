// Package crosspkg_union reaches a sealed-union field through a struct
// declared in another package. It also declares its own sealed Shape, so a
// field resolved against the wrong package names the wrong union.
package crosspkg_union

import shapes "github.com/tylergannon/polytype/internal/builder/testfixtures/crosspkg_shapes"

// Shape is this package's own sealed union, unrelated to shapes.Shape.
type Shape interface{ localShape() }

// Square is the only local shape.
type Square struct {
	Side int `json:"side"`
}

func (Square) localShape() {}

// Named reaches shapes.Holder through a named field.
type Named struct {
	H shapes.Holder `json:"h"`
}

// Embedded promotes shapes.Plain's union field next to a local one.
// (Embedding shapes.Holder is refused: its own generated MarshalJSON would be
// promoted alongside Embedded's.)
type Embedded struct {
	shapes.Plain
	Local Shape `json:"local"`
}

// Many reaches shapes.Holder through a slice.
type Many struct {
	Items []shapes.Holder `json:"items"`
}
