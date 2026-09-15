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
		polytype.Declare[model.MutualA](),
		polytype.Declare[model.MutualB](),
		polytype.SealedUnion[model.Block]("type"),
	)
	if err := codegen.Gen(config,
		codegen.Target("./model"),
		codegen.GoJSON(),
		codegen.TypeScript("./generated/typescript", true),
		codegen.Devalue("./generated/codec/codec_gen.go", "codec", "recursivefixture/generated/codec"),
	); err != nil {
		log.Fatal(err)
	}
}
