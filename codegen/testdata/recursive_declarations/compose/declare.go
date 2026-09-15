//go:build jsonschema

package compose

import "github.com/tylergannon/polytype"

var _ = polytype.Compose(
	polytype.Declare(Tree.Schema),
	polytype.SealedUnion[Node]("kind", polytype.Snake),
)
