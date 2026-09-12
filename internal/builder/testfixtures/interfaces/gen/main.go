package main

import (
	"log"

	"github.com/tylergannon/polytype/internal/builder"
)

func main() {
	err := builder.Run(builder.BuilderArgs{
		TargetDir: ".",
		Pretty:    true,
	})
	if err != nil {
		log.Fatal(err)
	}
}
