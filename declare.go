package polytype

import (
	"encoding/json"
	"strings"
)

// Declaration is an executable description of one generated schema root.
// It is both the value used by build-tagged declaration files and the value
// accepted by the programmatic code-generation API.
type Declaration[T any] struct {
	spec ConfigurationSpec
}

// Declare selects T as a generation root. With no argument it does not imply
// JSON Schema or an accessor method. Passing a method expression or free
// function records its name for schema-accessor generation.
//
// Both forms produce an ordinary configuration value:
//
//	config := polytype.Declare[Person]()
//	config := polytype.Declare(Person.Schema)
func Declare[T any](entrypoint ...func(T) json.RawMessage) *Declaration[T] {
	spec := DeclarationSpec{}
	var err error
	spec.Type, err = namedType[T]()
	if err == nil && len(entrypoint) > 1 {
		err = errTooManyEntrypoints
	}
	if err == nil && len(entrypoint) == 1 {
		var fullName string
		spec.EntrypointName, fullName, err = callableIdentity(entrypoint[0])
		if err == nil {
			spec.EntrypointFunc = !strings.Contains(fullName, "."+spec.Type.Name+".") &&
				!strings.Contains(fullName, ".(*"+spec.Type.Name+").")
		}
	}
	return &Declaration[T]{spec: ConfigurationSpec{Declarations: []DeclarationSpec{spec}, err: err}}
}

func (d *Declaration[T]) polytypeConfiguration() ConfigurationSpec {
	if d == nil {
		return ConfigurationSpec{err: errNilDeclaration}
	}
	return d.spec
}

func (d *Declaration[T]) withRule(rule RuleSpec, err error) *Declaration[T] {
	if d == nil {
		return &Declaration[T]{spec: ConfigurationSpec{err: errNilDeclaration}}
	}
	next := d.spec.clone()
	if next.err == nil && err != nil {
		next.err = err
	}
	if err == nil {
		next.Declarations[0].Rules = append(next.Declarations[0].Rules, rule)
	}
	return &Declaration[T]{spec: next}
}

// Accessor registers a provider for field that is a struct method taking
// only the receiver T (equivalent to WithStructAccessorMethod).
func (d *Declaration[T]) Accessor(field any, provider func(T) json.Marshaler) *Declaration[T] {
	ref, err := resolveField[T](field)
	if err != nil {
		return d.withRule(RuleSpec{}, err)
	}
	name, err := callableName(provider)
	return d.withRule(RuleSpec{Kind: RuleAccessor, Field: ref, ProviderName: name, ProviderIsMethod: true}, err)
}

// Method registers a provider for field that is a struct method also taking
// the field's own value F (equivalent to WithStructFunctionMethod). field and
// provider must agree on F: passing a field of one type alongside a provider
// expecting another fails to compile.
func (d *Declaration[T]) Method[F any](field any, provider func(T, F) json.Marshaler) *Declaration[T] {
	ref, err := resolveField[T](field)
	if err != nil {
		return d.withRule(RuleSpec{}, err)
	}
	name, err := callableName(provider)
	return d.withRule(RuleSpec{Kind: RuleMethod, Field: ref, ProviderName: name, ProviderIsMethod: true}, err)
}

// Function registers a provider for field that is a free function taking
// only the field's own value F (equivalent to WithFunction). field and
// provider must agree on F: passing a field of one type alongside a provider
// expecting another fails to compile.
func (d *Declaration[T]) Function[F any](field any, provider func(F) json.Marshaler) *Declaration[T] {
	ref, err := resolveField[T](field)
	if err != nil {
		return d.withRule(RuleSpec{}, err)
	}
	name, err := callableName(provider)
	return d.withRule(RuleSpec{Kind: RuleFunction, Field: ref, ProviderName: name}, err)
}

// StringerEnum marks field as an enum whose values are compared via
// fmt.Stringer (equivalent to WithStringerEnum).
func (d *Declaration[T]) StringerEnum(field any) *Declaration[T] {
	ref, err := resolveField[T](field)
	return d.withRule(RuleSpec{Kind: RuleStringerEnum, Field: ref}, err)
}

// Ref requests that, wherever T is referenced from another registered
// schema, it be rendered as a "$ref" into that schema's "$defs" instead of
// being inlined (equivalent to AsRef).
func (d *Declaration[T]) Ref() *Declaration[T] {
	return d.withRule(RuleSpec{Kind: RuleRef}, nil)
}

// RenderProviders requests generation of RenderedSchema() and provider
// execution at runtime (equivalent to WithRenderProviders).
func (d *Declaration[T]) RenderProviders() *Declaration[T] {
	return d.withRule(RuleSpec{Kind: RuleRenderProviders}, nil)
}
