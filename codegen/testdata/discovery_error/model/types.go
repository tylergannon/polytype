package model

// Root has a nested Child whose Block field uses a fixed-length array,
// which is unsupported for registered interface containers.
type Root struct {
	Child Child `json:"child"`
}

type Child struct {
	Blocks [1]Block `json:"blocks"`
}

type Block interface{ block() }

type Leaf struct {
	Text string `json:"text"`
}

func (Leaf) block() {}
