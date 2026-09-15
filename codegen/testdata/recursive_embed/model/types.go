package model

// Container embeds itself through a pointer and has a sealed-union field.
// This exercises the recursive embedding walk in resolveLocalInterfaceProps.
type Container struct {
	*Container
	Name   string  `json:"name"`
	Blocks []Block `json:"blocks"`
}

type Block interface{ block() }

type Leaf struct {
	Text string `json:"text"`
}

func (Leaf) block() {}

type Branch struct {
	Label string `json:"label"`
}

func (Branch) block() {}
