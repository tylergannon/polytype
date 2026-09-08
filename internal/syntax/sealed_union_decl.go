package syntax

import (
	"fmt"
	"go/token"
	"strconv"
	"unicode/utf8"

	"github.com/dave/dst"
	"github.com/tylergannon/polytype/internal/inflection"
)

// sealedUnionDeclaration is one parsed SealedUnion[I](discriminator) marker.
type sealedUnionDeclaration struct {
	Discriminator string
	Inflection    string
	Position      token.Position
}

// parseSealedUnionDeclaration validates the shape of one SealedUnion marker
// call: a type argument naming a type of this package, exactly one string
// literal argument holding a nonempty valid UTF-8 property name, an optional
// built-in inflection function, and no earlier declaration for the same
// interface.
func (r *ScanResult) parseSealedUnionDeclaration(decl MarkerFunctionCall, declarations map[string]sealedUnionDeclaration) error {
	position := decl.CallExpr.Position()
	target := decl.TypeArgument()
	if target == nil {
		return fmt.Errorf("polytype.SealedUnion at %s requires an interface type argument: SealedUnion[I](discriminator)", position)
	}
	if target.PkgPath != r.Pkg.PkgPath {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s must be declared in package %s, which declares the interface, not in package %s", target.TypeName, position, target.PkgPath, r.Pkg.PkgPath)
	}
	if target.Indirection != NormalConcrete {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s: the type argument must name an interface type directly", target.TypeName, position)
	}
	args := decl.CallExpr.Args()
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s expects a string literal discriminator and optional polytype.Snake, polytype.Camel, or polytype.Pascal inflector", target.TypeName, position)
	}
	literal, ok := args[0].Expr().(*dst.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s: the discriminator must be a string literal", target.TypeName, position)
	}
	discriminator, err := strconv.Unquote(literal.Value)
	if err != nil {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s: invalid discriminator literal: %w", target.TypeName, position, err)
	}
	if discriminator == "" || !utf8.ValidString(discriminator) {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s: the discriminator must be a nonempty valid UTF-8 property name", target.TypeName, position)
	}
	inflectionName := "Pascal"
	if len(args) == 2 {
		identity := parseFuncFromExpr(args[1])
		if identity.PkgPath != SchemaPackagePath || (identity.TypeName != "Snake" && identity.TypeName != "Camel" && identity.TypeName != "Pascal") {
			return fmt.Errorf("polytype.SealedUnion[%s] at %s: build-tagged declarations require polytype.Snake, polytype.Camel, or polytype.Pascal; arbitrary functions are supported by executable codegen configuration", target.TypeName, position)
		}
		inflectionName = identity.TypeName
	}
	if previous, exists := declarations[target.TypeName]; exists {
		return fmt.Errorf("polytype.SealedUnion[%s] at %s duplicates the declaration at %s; declare the discriminator once per interface", target.TypeName, position, previous.Position)
	}
	declarations[target.TypeName] = sealedUnionDeclaration{Discriminator: discriminator, Inflection: inflectionName, Position: position}
	return nil
}

// applySealedUnionDeclarations attaches each declared discriminator to its
// inferred sealed union, or reports why the target is not one.
func (r *ScanResult) applySealedUnionDeclarations(declarations map[string]sealedUnionDeclaration) error {
	for name, declaration := range declarations {
		if iface, ok := r.Interfaces[name]; ok {
			iface.Discriminator = declaration.Discriminator
			iface.DiscriminatorValues = make(map[string]string, len(iface.Impls))
			for _, impl := range iface.Impls {
				switch declaration.Inflection {
				case "Snake":
					iface.DiscriminatorValues[impl.TypeName] = inflection.Snake(impl.TypeName)
				case "Camel":
					iface.DiscriminatorValues[impl.TypeName] = inflection.Camel(impl.TypeName)
				case "Pascal":
					iface.DiscriminatorValues[impl.TypeName] = inflection.Pascal(impl.TypeName)
				}
			}
			r.Interfaces[name] = iface
			continue
		}
		if diagnostic, ok := r.InterfaceDiagnostics[name]; ok {
			return fmt.Errorf("polytype.SealedUnion[%s] at %s: %w", name, declaration.Position, diagnostic)
		}
		if _, ok := r.LocalNamedTypes[name]; ok {
			return fmt.Errorf("polytype.SealedUnion[%s] at %s: %s is not an interface type", name, declaration.Position, name)
		}
		if _, ok := r.Constants[name]; ok {
			return fmt.Errorf("polytype.SealedUnion[%s] at %s: %s is an enum type, not an interface type", name, declaration.Position, name)
		}
		return fmt.Errorf("polytype.SealedUnion[%s] at %s: %s is not declared in package %s", name, declaration.Position, name, r.Pkg.PkgPath)
	}
	return nil
}
