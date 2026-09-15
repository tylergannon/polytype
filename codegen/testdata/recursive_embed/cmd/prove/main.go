package main

import (
	"fmt"
	"log"
	"strings"

	"recursiveembedfixture/model"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"
)

func main() {
	config := polytype.Compose(
		polytype.Declare[model.Container](),
		polytype.SealedUnion[model.Block]("type"),
	)
	err := codegen.Gen(config, codegen.Target("./model"), codegen.GoJSON())
	if err == nil {
		log.Fatal("expected generation to fail for recursive embedding with competing MarshalJSON")
	}
	if !strings.Contains(err.Error(), "competing MarshalJSON") {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Println("generation correctly rejected recursive embedding")
}
