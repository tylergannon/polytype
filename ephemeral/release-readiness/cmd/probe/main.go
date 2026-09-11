package main

import (
	"encoding/json"
	"fmt"
	"example.com/polytype-readiness/model"
)

func main() {
	b, err := json.Marshal(model.Todo{})
	if err != nil { panic(err) }
	fmt.Printf("zero-value JSON: %s\n", b)
	fmt.Printf("validation: %v\n", (model.Todo{}).ValidateJSON(b))
}
