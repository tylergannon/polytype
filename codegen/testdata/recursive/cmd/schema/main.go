package main

import (
	"log"

	"recursivefixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	config := polytype.Compose(
		polytype.Declare[model.Tree](),
		polytype.Declare[model.Document](),
		polytype.SealedUnion[model.Block]("type"),
	)
	if err := codegen.Gen(config,
		codegen.Target("./model"),
		codegen.JSONSchema(),
	); err != nil {
		log.Fatal(err)
	}
}
