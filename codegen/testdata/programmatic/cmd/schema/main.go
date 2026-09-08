package main

import (
	"log"

	"programmaticfixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	config := polytype.Compose(
		polytype.Declare[model.Envelope](),
		polytype.SealedUnion[model.Event]("kind", polytype.Camel),
	)
	if err := codegen.Gen(config,
		codegen.Target("./model"),
		codegen.JSONSchema(),
		codegen.Pretty(),
	); err != nil {
		log.Fatal(err)
	}
}
