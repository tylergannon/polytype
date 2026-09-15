package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"

	"collisionfixture/dep"
	"collisionfixture/model"
)

func main() {
	value := model.Root{
		External: dep.Item{Name: "external"},
		Local: model.Item{
			Name: "local",
			Children: []model.Block{
				model.Branch{Children: []model.Block{model.Leaf{Text: "nested"}}},
			},
		},
	}
	data, err := json.Marshal(value)
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte(`"type"`)) {
		log.Fatal("recursive union discriminators are absent")
	}
	fmt.Println("ok")
}
