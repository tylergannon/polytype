package model

import "collisionfixture/dep"

// Root has two fields whose types share the bare name "Item" from different packages.
// dep.Item is a plain struct; model.Item contains a recursive sealed-union field.
type Root struct {
	External dep.Item `json:"external"`
	Local    Item     `json:"local"`
}

// Item is the local type. Its Children field is a sealed union.
type Item struct {
	Name     string  `json:"name"`
	Children []Block `json:"children"`
}

type Block interface{ block() }

type Leaf struct {
	Text string `json:"text"`
}

func (Leaf) block() {}

type Branch struct {
	Children []Block `json:"children"`
}

func (Branch) block() {}
