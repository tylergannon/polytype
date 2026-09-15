package model

import "github.com/tylergannon/polytype"

// Tree is a recursive structure: each node names itself and holds children.
type Tree struct {
	Name     string                   `json:"name"`
	Children []Tree                   `json:"children"`
	Parent   polytype.Nullable[*Tree] `json:"parent"`
	Metadata polytype.Optional[*Tree] `json:"metadata,omitzero"`
}

// Document is a recursive sealed-union tree: a Block contains nested content.
type Document struct {
	Title   string  `json:"title"`
	Content []Block `json:"content"`
}

type Block interface{ block() }

// Paragraph is a leaf block.
type Paragraph struct {
	Text string `json:"text"`
}

func (Paragraph) block() {}

// Section is a recursive block that contains other blocks.
type Section struct {
	Heading  string  `json:"heading"`
	Children []Block `json:"children"`
}

func (Section) block() {}

// MutualA and MutualB demonstrate mutual recursion through pointers.
type MutualA struct {
	Value int                         `json:"value"`
	Peer  polytype.Nullable[*MutualB] `json:"peer"`
}

type MutualB struct {
	Label string                      `json:"label"`
	Peer  polytype.Nullable[*MutualA] `json:"peer"`
}
