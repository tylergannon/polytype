package polytype

// SealedUnionMarker is executable configuration for one sealed interface.
type SealedUnionMarker struct {
	spec ConfigurationSpec
}

func (m SealedUnionMarker) polytypeConfiguration() ConfigurationSpec { return m.spec }

// SealedUnion declares the discriminator property for the sealed interface
// I. A sealed interface is one whose own body declares an unexported method;
// its variants are inferred from the same-package struct types that declare
// that method directly, so membership needs no declaration. The
// discriminator is the only per-union setting: the default property is
// "type" and needs no declaration; SealedUnion sets a different property for
// every use of I in every generated schema, codec, and TypeScript output.
// Discriminator values are unchanged: the concrete type name.
//
// The declaration must appear in the build-tagged file of the package that
// declares I, exactly once per interface, with a string literal argument:
//
//	var _ = polytype.SealedUnion[Animal]("kind")
//
// A declaration in another package, a duplicate declaration, a declaration
// for a non-sealed interface or a non-interface type, a non-literal argument,
// or an invalid property name is a generation error naming the interface.
func SealedUnion[I any](discriminator string) SealedUnionMarker {
	typ, err := namedType[I]()
	if err == nil {
		err = validateDiscriminator(discriminator)
	}
	return SealedUnionMarker{spec: ConfigurationSpec{
		SealedUnions: []SealedUnionSpec{{Type: typ, Discriminator: discriminator}},
		err:          err,
	}}
}
