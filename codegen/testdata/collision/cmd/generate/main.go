package main

import (
	"log"

	"collisionfixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	config := polytype.Compose(
		polytype.Declare[model.Root](),
		polytype.SealedUnion[model.Block]("type"),
	)
	if err := codegen.Gen(config, codegen.Target("./model"), codegen.GoJSON()); err != nil {
		log.Fatal(err)
	}
}
