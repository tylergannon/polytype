package builder

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typescript"
)

type BuilderArgs struct {
	TargetDir string
	Pretty    bool
	NoChanges bool // If true, fail if any schema changes are detected
	Force     bool // If true, force regeneration and permit removal of generated validation methods
	Validate  bool // If true, generate validation methods and schema compilation
	// TypeScriptDir selects a directory for structural TypeScript declarations.
	// Relative paths are resolved against the invocation working directory.
	TypeScriptDir    string
	TypeScriptBarrel bool
}

func Run(args BuilderArgs) (err error) {
	if args.TypeScriptBarrel && args.TypeScriptDir == "" {
		return fmt.Errorf("--typescript-barrel requires --typescript")
	}
	var (
		pkgs    []*decorator.Package
		builder SchemaBuilder
	)
	if pkgs, err = syntax.Load(args.TargetDir); err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found in %s", args.TargetDir)
	}
	if builder, err = New(pkgs[0]); err != nil {
		return err
	}
	if err = guardValidationMethodRemoval(builder.Scan.Pkg.Dir, args); err != nil {
		return err
	}
	builder.Pretty = args.Pretty
	builder.Validate = args.Validate

	// A NewJSONSchemaBuilder registration's stub takes no arguments; if its
	// receiver type's underlying type is a pointer or interface, Go forbids
	// a method there, and the zero-argument signature can't be preserved as
	// a free function without risking a name collision (the same builder
	// function reused for two such types) -- reject instead of
	// miscompiling or silently dropping it.
	for _, fn := range builder.InvalidReceiverBuilderRoots() {
		return fmt.Errorf("%s: NewJSONSchemaBuilder is not supported for a type whose underlying type is a pointer or interface (Go forbids declaring a method on it); register a one-argument free function via Declare or NewJSONSchemaFunc instead", fn.Receiver.TypeName)
	}

	// A free-function schema root whose underlying type is a pointer or
	// interface can't have any method declared on it, so modes that require
	// one (RenderedSchema for RenderProviders(), ValidateJSON for
	// --validate) can't be generated for it. Reject clearly rather than
	// silently producing incomplete output.
	for _, fn := range builder.SchemaFreeFuncs() {
		name := fn.Receiver.TypeName
		if builder.Rendered[name] {
			return fmt.Errorf("%s: RenderProviders() is not supported for a free-function schema root whose underlying type is a pointer or interface (Go forbids declaring a method on it, so no RenderedSchema() can be generated); drop RenderProviders() for this type", name)
		}
		if builder.Validate {
			return fmt.Errorf("--validate cannot generate ValidateJSON for %s: its schema entrypoint is a free function because its underlying type is a pointer or interface, and Go forbids declaring any method (so ValidateJSON) on it; remove --validate or drop this type's registration", name)
		}
	}

	// Allow registered transforms to mutate the model before render (no-ops by default)
	if err = (&builder).applyTransforms(); err != nil {
		return err
	}

	// Lower, render, and preflight all TypeScript outputs before any output is
	// mutated. In particular, an unsupported source shape or an unowned output
	// collision must leave ordinary generated artifacts untouched.
	var typeScriptPlan *typescriptOutputPlan
	if args.TypeScriptDir != "" {
		definitions, definitionsErr := (&builder).TypeDefinitions()
		if definitionsErr != nil {
			return fmt.Errorf("generate TypeScript definitions: %w", definitionsErr)
		}
		result, generateErr := typescript.Generate(definitions, typescript.Options{Barrel: args.TypeScriptBarrel})
		if generateErr != nil {
			return fmt.Errorf("generate TypeScript output: %w", generateErr)
		}
		typeScriptPlan, err = prepareTypeScriptOutput(args.TypeScriptDir, result.Files, args.TypeScriptBarrel)
		if err != nil {
			return err
		}
	}

	var changedSchemas map[string]bool
	if changedSchemas, err = builder.RenderSchemas(args.NoChanges, args.Force); err != nil {
		return err
	}

	// If NoChanges is set, fail if any schemas changed
	if args.NoChanges {
		var changedTypes []string
		for typeName, changed := range changedSchemas {
			if changed {
				changedTypes = append(changedTypes, typeName)
			}
		}
		if len(changedTypes) > 0 {
			slices.Sort(changedTypes)
			return fmt.Errorf("schema changes detected for types: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(changedTypes, ", "))
		}
		if typeScriptPlan != nil && typeScriptPlan.changed() {
			return fmt.Errorf("TypeScript output changes detected for paths: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(typeScriptPlan.changedPaths(), ", "))
		}
	}

	if err = builder.RenderGoCode(); err != nil {
		return err
	}
	if typeScriptPlan != nil {
		if err = typeScriptPlan.apply(args.Force); err != nil {
			return err
		}
	}
	return nil
}
