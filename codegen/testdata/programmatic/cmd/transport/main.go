package main

import (
	"log"

	"programmaticfixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	config := polytype.Declare[model.Envelope]()
	if err := codegen.Gen(config,
		codegen.Target("./model"),
		codegen.TypeScript("./generated/typescript", true),
		codegen.Devalue("./generated/codec/codec_gen.go", "codec", "programmaticfixture/generated/codec"),
	); err != nil {
		log.Fatal(err)
	}
}
