//go:build jsonschema

package stringer_enums

import (
	"encoding/json"

	"github.com/tylergannon/polytype"
)

// ApplicationConfig schema with WithStringerEnum (WITHOUT NewEnumType - testing auto-discovery!)
func (ApplicationConfig) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(ApplicationConfig.Schema).
	StringerEnum(polytype.Field[ApplicationConfig, LogLevel]("LogLevel")).
	StringerEnum(polytype.Field[ApplicationConfig, Priority]("DefaultPriority"))

// Task uses the same enum types in value mode: Priority and LogLevel declare
// `func (T) enum()` in types.go, so their fields emit integer values with no
// field-level registration.
func (Task) Schema() json.RawMessage { panic("not implemented") }

var _ = polytype.Declare(Task.Schema)
