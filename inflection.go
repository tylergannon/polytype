package polytype

import "github.com/tylergannon/polytype/internal/inflection"

// Pascal returns a Go type name unchanged. It is the default discriminator
// inflection and preserves the historical concrete-type-name wire value.
func Pascal(name string) string { return inflection.Pascal(name) }

// Camel converts a Go type name to lower camelCase for use as a discriminator
// value.
func Camel(name string) string { return inflection.Camel(name) }

// Snake converts a Go type name to lower snake_case for use as a discriminator
// value.
func Snake(name string) string { return inflection.Snake(name) }
