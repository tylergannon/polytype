package main

import (
	"encoding/json"
	"fmt"
	"os"

	"recursivefixture/generated/codec"
	"recursivefixture/model"
)

type packet struct {
	TreeDevalue     string          `json:"tree_devalue"`
	DocumentDevalue string          `json:"document_devalue"`
	DocumentJSON    json.RawMessage `json:"document_json,omitempty"`
}

func values() (model.Tree, model.Document) {
	tree := model.Tree{
		Name: "root",
		Children: []model.Tree{
			{Name: "child", Children: []model.Tree{{Name: "grandchild", Children: []model.Tree{}}}},
		},
	}
	doc := model.Document{
		Title: "interop",
		Content: []model.Block{
			model.Paragraph{Text: "intro"},
			model.Section{
				Heading: "chapter",
				Children: []model.Block{
					model.Paragraph{Text: "body"},
					model.Section{Heading: "sub", Children: []model.Block{}},
				},
			},
		},
	}
	return tree, doc
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: interop emit|consume")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "emit":
		tree, doc := values()
		td, err := codec.StringifyTree(tree)
		if err != nil {
			panic(err)
		}
		dd, err := codec.StringifyDocument(doc)
		if err != nil {
			panic(err)
		}
		dj, err := json.Marshal(doc)
		if err != nil {
			panic(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(packet{TreeDevalue: td, DocumentDevalue: dd, DocumentJSON: dj}); err != nil {
			panic(err)
		}
	case "consume":
		var p packet
		if err := json.NewDecoder(os.Stdin).Decode(&p); err != nil {
			panic(err)
		}
		tree, err := codec.ParseTree(p.TreeDevalue)
		if err != nil {
			panic(err)
		}
		doc, err := codec.ParseDocument(p.DocumentDevalue)
		if err != nil {
			panic(err)
		}
		treeJSON, err := json.Marshal(tree)
		if err != nil {
			panic(err)
		}
		docJSON, err := json.Marshal(doc)
		if err != nil {
			panic(err)
		}
		fmt.Printf(`{"tree":%s,"document":%s}`, treeJSON, docJSON)
		fmt.Println()
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", os.Args[1])
		os.Exit(1)
	}
}
