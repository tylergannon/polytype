//go:build jsonschema

// Package fluentfieldmismatch is a standalone negative fixture: its
// .StringerEnum(...) chain link names a field on a type other than the Declare
// root, which must be a hard scanner error, not a silent skip. It lives in
// its own directory (rather than testfixtures/typescanner, which every
// happy-path fluent scanner test shares) because loading a package eagerly
// parses every marker call in it, and this package is expected to fail that
// parse.
package fluentfieldmismatch

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

type Owner struct {
	A string
}

type Other struct {
	X string
}

func (Owner) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Owner.Schema).
	StringerEnum(polytype.Field[Other, string]("X"))
