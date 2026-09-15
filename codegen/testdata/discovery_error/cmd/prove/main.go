package main

import (
	"fmt"
	"log"

	"discoveryerrorfixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	_ = model.Root{}
	config := polytype.Compose(
		polytype.Declare[model.Root](),
		polytype.SealedUnion[model.Block]("type"),
	)
	err := codegen.Gen(config, codegen.Target("./model"), codegen.GoJSON())
	if err != nil {
		fmt.Println("generation correctly rejected:", err)
		return
	}
	log.Fatal("expected generation to fail for unsupported interface container")
}
