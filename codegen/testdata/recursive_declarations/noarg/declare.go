//go:build jsonschema

package noarg

import "github.com/tylergannon/polytype"

var _ = polytype.Declare[Tree]()
var _ = polytype.SealedUnion[Node]("kind", polytype.Snake)
