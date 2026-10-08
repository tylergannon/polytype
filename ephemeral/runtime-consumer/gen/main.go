package main

import (
	"log"

	"example.com/runtime-consumer/model"
	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	if err := codegen.Gen(polytype.Declare[model.Packet](),
		codegen.Target("./model"),
		codegen.Devalue("./codec/codec_gen.go", "codec", "example.com/runtime-consumer/codec"),
	); err != nil {
		log.Fatal(err)
	}
}
