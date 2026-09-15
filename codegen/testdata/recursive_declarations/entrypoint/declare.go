//go:build jsonschema

package entrypoint

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

func (Tree) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Tree.Schema)
var _ = polytype.SealedUnion[Node]("kind", polytype.Snake)
