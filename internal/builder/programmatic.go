package builder

import (
	"fmt"
	"unicode/utf8"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// ConfiguredRule is one declaration rule supplied through the public
// programmatic configuration API.
type ConfiguredRule struct {
	Kind             string
	FieldName        string
	ProviderName     string
	ProviderIsMethod bool
}

// ConfiguredDeclaration selects one named root without requiring a marker in
// the target package's source.
type ConfiguredDeclaration struct {
	PackagePath    string
	TypeName       string
	Pointer        bool
	Entrypoint     string
	EntrypointFunc bool
	Rules          []ConfiguredRule
}

// ConfiguredUnion supplies the optional discriminator override for an
// inferred sealed interface.
type ConfiguredUnion struct {
	PackagePath   string
	TypeName      string
	Discriminator string
	Inflect       func(string) string
}

// ProgrammaticConfig is the builder-facing form of executable declarations.
type ProgrammaticConfig struct {
	Declarations []ConfiguredDeclaration
	SealedUnions []ConfiguredUnion
}

// LoadProgrammatic loads target and constructs the existing generation engine
// from executable declarations. discoverCodecs walks types to populate the
// enum and sealed-union codec metadata needed by GoJSON. mapSchemas builds
// JSON Schema nodes; it implies discoverCodecs.
func LoadProgrammatic(target string, config ProgrammaticConfig, discoverCodecs, mapSchemas bool) (SchemaBuilder, error) {
	pkgs, err := syntax.Load(target)
	if err != nil {
		return SchemaBuilder{}, err
	}
	if len(pkgs) == 0 {
		return SchemaBuilder{}, fmt.Errorf("no packages found for %s", target)
	}
	return NewProgrammatic(pkgs[0], config, discoverCodecs, mapSchemas)
}

// NewProgrammatic constructs the existing generation engine from executable
// declarations instead of source marker calls. Root declarations in the
// package's source are not read, so a declaration file for the CLI never
// affects or fails a programmatic run; its SealedUnion markers still apply
// unless config overrides them.
func NewProgrammatic(pkg *decorator.Package, config ProgrammaticConfig, discoverCodecs, mapSchemas bool) (SchemaBuilder, error) {
	data, err := syntax.LoadConfiguredPackage(pkg)
	if err != nil {
		return SchemaBuilder{}, err
	}

	for _, union := range config.SealedUnions {
		if union.PackagePath != data.Pkg.PkgPath {
			return SchemaBuilder{}, fmt.Errorf("configured sealed union %s.%s is outside target package %s", union.PackagePath, union.TypeName, data.Pkg.PkgPath)
		}
		if union.Discriminator == "" || !utf8.ValidString(union.Discriminator) {
			return SchemaBuilder{}, fmt.Errorf("configured sealed union %s: discriminator must be a nonempty valid UTF-8 property name", union.TypeName)
		}
		iface, ok := data.Interfaces[union.TypeName]
		if !ok {
			return SchemaBuilder{}, fmt.Errorf("configured sealed union %s is not a sealed interface in %s", union.TypeName, data.Pkg.PkgPath)
		}
		iface.Discriminator = union.Discriminator
		values, err := discriminatorValues(union, iface)
		if err != nil {
			return SchemaBuilder{}, err
		}
		iface.DiscriminatorValues = values
		data.Interfaces[union.TypeName] = iface
	}

	for _, declaration := range config.Declarations {
		if declaration.PackagePath != data.Pkg.PkgPath {
			return SchemaBuilder{}, fmt.Errorf("configured root %s.%s is outside target package %s", declaration.PackagePath, declaration.TypeName, data.Pkg.PkgPath)
		}
		if declaration.TypeName == "" {
			return SchemaBuilder{}, fmt.Errorf("configured root in %s has an empty type name", data.Pkg.PkgPath)
		}
		if err := data.EnsureType(declaration.PackagePath, declaration.TypeName); err != nil {
			return SchemaBuilder{}, fmt.Errorf("resolve configured root %s: %w", declaration.TypeName, err)
		}
		receiver := syntax.TypeID{PkgPath: declaration.PackagePath, TypeName: declaration.TypeName}
		if declaration.Pointer {
			receiver.Indirection = syntax.Pointer
		}
		method := syntax.SchemaMethod{
			Receiver:         receiver,
			SchemaMethodName: declaration.Entrypoint,
			Options:          make([]syntax.SchemaMethodOptionInfo, 0, len(declaration.Rules)),
		}
		for _, rule := range declaration.Rules {
			method.Options = append(method.Options, syntax.SchemaMethodOptionInfo{
				Kind:             syntax.SchemaMethodOptionKind(rule.Kind),
				FieldName:        rule.FieldName,
				ProviderName:     rule.ProviderName,
				ProviderIsMethod: rule.ProviderIsMethod,
			})
		}
		if declaration.EntrypointFunc {
			data.SchemaFuncs = append(data.SchemaFuncs, syntax.SchemaFunction(method))
		} else {
			data.SchemaMethods = append(data.SchemaMethods, method)
		}
	}
	schemas := noSchemas
	if mapSchemas {
		schemas = allSchemas
	}
	return newFromScan(data, nil, discoverCodecs, schemas)
}

func discriminatorValues(union ConfiguredUnion, iface syntax.IfaceImplementations) (values map[string]string, err error) {
	inflect := union.Inflect
	if inflect == nil {
		inflect = func(name string) string { return name }
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("configured sealed union %s: discriminator inflection panicked: %v", union.TypeName, recovered)
			values = nil
		}
	}()
	values = make(map[string]string, len(iface.Impls))
	seen := make(map[string]string, len(iface.Impls))
	for _, impl := range iface.Impls {
		value := inflect(impl.TypeName)
		if value == "" || !utf8.ValidString(value) {
			return nil, fmt.Errorf("configured sealed union %s: discriminator inflection returned an empty or invalid UTF-8 value for variant %s", union.TypeName, impl.TypeName)
		}
		if previous, exists := seen[value]; exists {
			return nil, fmt.Errorf("configured sealed union %s: discriminator inflection returned duplicate value %q for variants %s and %s", union.TypeName, value, previous, impl.TypeName)
		}
		seen[value] = impl.TypeName
		values[impl.TypeName] = value
	}
	return values, nil
}

// ApplyTransforms applies registered builder transforms before any output is
// rendered.
func (s *SchemaBuilder) ApplyTransforms() error { return s.applyTransforms() }

// ConfiguredTypeDefinitions returns the executable declaration roots' shared
// grammar and one root node per declaration, for the library backends.
func (s *SchemaBuilder) ConfiguredTypeDefinitions() (typegrammar.Definitions, []typegrammar.Type, error) {
	defs, err := s.TypeDefinitions()
	if err != nil {
		return nil, nil, err
	}
	methods := s.roots()
	roots := make([]typegrammar.Type, 0, len(methods))
	for _, method := range methods {
		var root typegrammar.Type = &typegrammar.Ref{Target: typegrammar.Name{PackagePath: method.Receiver.PkgPath, Name: method.Receiver.TypeName}}
		if method.Receiver.Indirection == syntax.Pointer {
			root = &typegrammar.Pointer{Element: root}
		}
		roots = append(roots, root)
	}
	return defs, roots, nil
}
