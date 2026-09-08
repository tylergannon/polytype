package builder

import (
	"fmt"
	"go/types"
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
}

// ProgrammaticConfig is the builder-facing form of executable declarations.
type ProgrammaticConfig struct {
	Declarations []ConfiguredDeclaration
	SealedUnions []ConfiguredUnion
}

// LoadProgrammatic loads target and constructs the existing generation engine
// from executable declarations.
func LoadProgrammatic(target string, config ProgrammaticConfig, mapSchemas bool) (SchemaBuilder, error) {
	pkgs, err := syntax.Load(target)
	if err != nil {
		return SchemaBuilder{}, err
	}
	if len(pkgs) == 0 {
		return SchemaBuilder{}, fmt.Errorf("no packages found for %s", target)
	}
	return NewProgrammatic(pkgs[0], config, mapSchemas)
}

// NewProgrammatic constructs the existing generation engine from executable
// declarations instead of source marker calls.
func NewProgrammatic(pkg *decorator.Package, config ProgrammaticConfig, mapSchemas bool) (SchemaBuilder, error) {
	data, err := syntax.LoadPackage(pkg)
	if err != nil {
		return SchemaBuilder{}, err
	}
	data.SchemaMethods = nil
	data.SchemaFuncs = nil

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
	return newFromScan(data, nil, mapSchemas)
}

// ApplyTransforms applies registered builder transforms before any output is
// rendered.
func (s *SchemaBuilder) ApplyTransforms() error { return s.applyTransforms() }

// ConfiguredTypeDefinitions lowers the executable declaration roots and
// returns both the shared grammar and its roots for library backends.
func (s *SchemaBuilder) ConfiguredTypeDefinitions() (typegrammar.Definitions, []typegrammar.Type, error) {
	roots := make([]RootType, 0, len(s.Scan.SchemaMethods)+len(s.Scan.SchemaFuncs))
	appendRoot := func(method syntax.SchemaMethod) error {
		obj := s.Scan.Pkg.Types.Scope().Lookup(method.Receiver.TypeName)
		if obj == nil {
			return fmt.Errorf("configured root type %s is not declared in %s", method.Receiver.TypeName, s.Scan.Pkg.PkgPath)
		}
		typ := obj.Type()
		if method.Receiver.Indirection == syntax.Pointer {
			typ = types.NewPointer(typ)
		}
		roots = append(roots, RootType{Type: typ, Position: s.Scan.Pkg.Fset.Position(obj.Pos())})
		return nil
	}
	for _, method := range s.Scan.SchemaMethods {
		if err := appendRoot(method); err != nil {
			return nil, nil, err
		}
	}
	for _, method := range s.Scan.SchemaFuncs {
		if err := appendRoot(syntax.SchemaMethod(method)); err != nil {
			return nil, nil, err
		}
	}
	return s.LowerRoots(roots)
}
