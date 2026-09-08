package polytype

import "fmt"

// SealedUnionMarker is executable configuration for one sealed interface.
type SealedUnionMarker struct {
	spec ConfigurationSpec
}

func (m SealedUnionMarker) polytypeConfiguration() ConfigurationSpec { return m.spec }

// SealedUnion declares the discriminator property for the sealed interface
// I. A sealed interface is one whose own body declares an unexported method;
// its variants are inferred from the same-package struct types that declare
// that method directly, so membership needs no declaration. The
// The default property is "type" and needs no declaration; SealedUnion sets a
// different property for every use of I in every generated schema, codec, and
// TypeScript output. The optional inflector determines each variant's value
// from its concrete Go type name. Pascal is the default; Snake and Camel are
// also provided. Executable codegen configuration may supply any non-nil
// func(string) string. Build-tagged source markers accept the named Pascal,
// Snake, and Camel functions.
//
// The declaration must appear in the build-tagged file of the package that
// declares I, exactly once per interface, with a string literal argument:
//
//	var _ = polytype.SealedUnion[Animal]("kind", polytype.Snake)
//
// A declaration in another package, a duplicate declaration, a declaration
// for a non-sealed interface or a non-interface type, a non-literal argument,
// or an invalid property name is a generation error naming the interface.
func SealedUnion[I any](discriminator string, inflectors ...func(string) string) SealedUnionMarker {
	typ, err := namedType[I]()
	if err == nil {
		err = validateDiscriminator(discriminator)
	}
	var inflect func(string) string
	if err == nil && len(inflectors) > 1 {
		err = fmt.Errorf("polytype.SealedUnion[%s] accepts at most one discriminator inflection function", typ.Name)
	}
	if err == nil && len(inflectors) == 1 {
		inflect = inflectors[0]
		if inflect == nil {
			err = fmt.Errorf("polytype.SealedUnion[%s]: discriminator inflection function must not be nil", typ.Name)
		}
	}
	return SealedUnionMarker{spec: ConfigurationSpec{
		SealedUnions: []SealedUnionSpec{{Type: typ, Discriminator: discriminator, Inflect: inflect}},
		err:          err,
	}}
}
