// Package crosspkg_shapes declares a sealed union and a struct holding it, for a
// generated package in another package to reach.
package crosspkg_shapes

// Shape is sealed by its unexported method.
type Shape interface{ shape() }

// Circle is a round shape.
type Circle struct {
	Radius int `json:"radius"`
}

func (Circle) shape() {}

// Holder holds a shape.
type Holder struct {
	S Shape `json:"s"`
}

// Plain holds a shape but is not a root here, so this package generates no
// codec for it and another package may embed it.
type Plain struct {
	S Shape `json:"s"`
}
