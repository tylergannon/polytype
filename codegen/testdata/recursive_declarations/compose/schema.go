//go:build jsonschema

package compose

import "encoding/json"

// Schema is the accessor stub that Declare(Tree.Schema) in declare.go names.
func (Tree) Schema() json.RawMessage { panic("not implemented") }
