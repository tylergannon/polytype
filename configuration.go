package polytype

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"unicode/utf8"
)

// Configuration is the common value accepted by polytype generators.
// Declarations, sealed-union settings, and Compose results implement it.
type Configuration interface {
	polytypeConfiguration() ConfigurationSpec
}

// RuleKind identifies one declaration rule.
type RuleKind string

const (
	RuleAccessor        RuleKind = "accessor"
	RuleMethod          RuleKind = "method"
	RuleFunction        RuleKind = "function"
	RuleStringerEnum    RuleKind = "stringer-enum"
	RuleRef             RuleKind = "ref"
	RuleRenderProviders RuleKind = "render-providers"
)

// TypeSpec identifies a named Go type without retaining a runtime value.
type TypeSpec struct {
	PackagePath string
	Name        string
	Pointer     bool
}

// FieldSpec identifies a field by owner and Go field name.
type FieldSpec struct {
	Owner TypeSpec
	Name  string
}

// FieldRef retains a field's identity and value type after a declaration is
// evaluated. The value type keeps provider bindings type-safe.
type FieldRef[F any] struct {
	field FieldSpec
	err   error
}

// Field returns a stable reference to a field of T.
func Field[T, F any](name string) FieldRef[F] {
	typ, err := namedType[T]()
	if err != nil {
		return FieldRef[F]{err: err}
	}
	base := reflect.TypeFor[T]()
	for base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	if base.Kind() != reflect.Struct {
		return FieldRef[F]{field: FieldSpec{Owner: typ, Name: name}, err: fmt.Errorf("polytype.Field[%s]: %s is not a struct", typ.Name, typ.Name)}
	}
	structField, ok := base.FieldByName(name)
	if !ok {
		return FieldRef[F]{field: FieldSpec{Owner: typ, Name: name}, err: fmt.Errorf("polytype.Field[%s]: no field named %q", typ.Name, name)}
	}
	fieldType := reflect.TypeFor[F]()
	if structField.Type != fieldType {
		return FieldRef[F]{
			field: FieldSpec{Owner: typ, Name: name},
			err:   fmt.Errorf("polytype.Field[%s, %s]: field %s has type %s", typ.Name, fieldType, name, structField.Type),
		}
	}
	return FieldRef[F]{field: FieldSpec{Owner: typ, Name: name}}
}

// RuleSpec is one executable declaration rule.
type RuleSpec struct {
	Kind             RuleKind
	Field            FieldSpec
	ProviderName     string
	ProviderIsMethod bool
}

// DeclarationSpec is the generator-facing form of a Declaration.
type DeclarationSpec struct {
	Type           TypeSpec
	EntrypointName string
	EntrypointFunc bool
	Rules          []RuleSpec
}

// SealedUnionSpec configures the discriminator property of one inferred
// sealed interface.
type SealedUnionSpec struct {
	Type          TypeSpec
	Discriminator string
}

// ConfigurationSpec is the resolved meaning of one or more configuration
// values. Generators obtain it with ResolveConfiguration.
type ConfigurationSpec struct {
	Declarations []DeclarationSpec
	SealedUnions []SealedUnionSpec
	err          error
}

func (s ConfigurationSpec) clone() ConfigurationSpec {
	next := s
	next.Declarations = append([]DeclarationSpec(nil), s.Declarations...)
	for i := range next.Declarations {
		next.Declarations[i].Rules = append([]RuleSpec(nil), next.Declarations[i].Rules...)
	}
	next.SealedUnions = append([]SealedUnionSpec(nil), s.SealedUnions...)
	return next
}

type configurationGroup struct{ spec ConfigurationSpec }

func (g configurationGroup) polytypeConfiguration() ConfigurationSpec { return g.spec }

// Compose combines declarations and union settings into one configuration.
func Compose(configs ...Configuration) Configuration {
	spec, err := ResolveConfiguration(configs...)
	spec.err = err
	return configurationGroup{spec: spec}
}

// ResolveConfiguration returns the complete meaning of configs and validates
// identities that must agree before source loading begins.
func ResolveConfiguration(configs ...Configuration) (ConfigurationSpec, error) {
	var out ConfigurationSpec
	for i, config := range configs {
		if config == nil {
			return ConfigurationSpec{}, fmt.Errorf("polytype configuration %d is nil", i)
		}
		spec := config.polytypeConfiguration()
		if spec.err != nil {
			return ConfigurationSpec{}, spec.err
		}
		out.Declarations = append(out.Declarations, spec.Declarations...)
		out.SealedUnions = append(out.SealedUnions, spec.SealedUnions...)
	}
	if len(out.Declarations) == 0 {
		return ConfigurationSpec{}, errors.New("polytype configuration has no declarations")
	}
	return out, nil
}

var (
	errNilDeclaration     = errors.New("polytype: nil declaration")
	errTooManyEntrypoints = errors.New("polytype.Declare accepts at most one schema entrypoint")
)

func namedType[T any]() (TypeSpec, error) {
	t := reflect.TypeFor[T]()
	pointer := false
	if t.Kind() == reflect.Pointer {
		pointer = true
		t = t.Elem()
	}
	if t.Name() == "" || t.PkgPath() == "" {
		return TypeSpec{}, fmt.Errorf("polytype declaration requires a named Go type, got %s", reflect.TypeFor[T]())
	}
	return TypeSpec{PackagePath: t.PkgPath(), Name: t.Name(), Pointer: pointer}, nil
}

func callableName(fn any) (string, error) {
	name, _, err := callableIdentity(fn)
	return name, err
}

func callableIdentity(fn any) (string, string, error) {
	v := reflect.ValueOf(fn)
	if !v.IsValid() || v.Kind() != reflect.Func || v.IsNil() {
		return "", "", errors.New("polytype: provider must be a non-nil function")
	}
	resolved := runtime.FuncForPC(v.Pointer())
	if resolved == nil {
		return "", "", errors.New("polytype: could not resolve provider function name")
	}
	fullName := resolved.Name()
	name := fullName
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	name = strings.TrimSuffix(name, "-fm")
	if name == "" {
		return "", "", errors.New("polytype: resolved an empty function name")
	}
	return name, fullName, nil
}

func resolveField[T, F any](field FieldRef[F]) (FieldSpec, error) {
	owner, err := namedType[T]()
	if err != nil {
		return FieldSpec{}, err
	}
	if field.err != nil {
		return FieldSpec{}, field.err
	}
	if field.field.Owner.PackagePath != owner.PackagePath || field.field.Owner.Name != owner.Name {
		return FieldSpec{}, fmt.Errorf("polytype: field %s.%s does not belong to declaration %s", field.field.Owner.Name, field.field.Name, owner.Name)
	}
	return field.field, nil
}

func validateDiscriminator(name string) error {
	if name == "" || !utf8.ValidString(name) {
		return errors.New("polytype: discriminator must be a nonempty valid UTF-8 property name")
	}
	return nil
}
