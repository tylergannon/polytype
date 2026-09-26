package builder

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/javascript"
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
	// JavaScriptDir selects a directory for JSDoc JavaScript declarations.
	// Relative paths are resolved against the invocation working directory.
	JavaScriptDir string
}

// sameOutputDirectory reports whether two declaration output directories
// resolve to the same location. An empty argument never matches.
func sameOutputDirectory(a, b string) (bool, error) {
	if a == "" || b == "" {
		return false, nil
	}
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, fmt.Errorf("resolve output directory %s: %w", a, err)
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false, fmt.Errorf("resolve output directory %s: %w", b, err)
	}
	return filepath.Clean(absA) == filepath.Clean(absB), nil
}

// validateDeclarationOutputArgs rejects flag combinations that cannot be
// honored: a barrel without TypeScript, and both declaration modes claiming
// one directory. The modes own different files, so sharing a directory would
// make each run delete the other's output.
func validateDeclarationOutputArgs(args BuilderArgs) error {
	if args.TypeScriptBarrel && args.TypeScriptDir == "" {
		return fmt.Errorf("--typescript-barrel requires --typescript")
	}
	same, err := sameOutputDirectory(args.TypeScriptDir, args.JavaScriptDir)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("--typescript and --javascript cannot target the same directory %s: the modes are exclusive per directory", args.TypeScriptDir)
	}
	return nil
}

// Run loads args.TargetDir and generates from it. args.TargetDir is used only
// for the load; every later step reads the loaded package, so a caller that
// already holds one can skip straight to RunLoaded.
func Run(args BuilderArgs) (err error) {
	if err = validateDeclarationOutputArgs(args); err != nil {
		return err
	}
	var pkgs []*decorator.Package
	if pkgs, err = syntax.Load(args.TargetDir); err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found in %s", args.TargetDir)
	}
	return RunLoaded(pkgs[0], args)
}

// RunLoaded generates from an already-loaded package, so many targets can share
// one packages.Load instead of paying a full dependency type-check each. It
// reads args.TargetDir for nothing: output locations come from the package's
// own directory.
//
// The caller keeps ownership of pkg. Generation writes files to disk but does
// not mutate the loaded graph, so one loaded package graph may back several
// concurrent RunLoaded calls for different packages in it.
func RunLoaded(pkg *decorator.Package, args BuilderArgs) (err error) {
	if err = validateDeclarationOutputArgs(args); err != nil {
		return err
	}
	var builder SchemaBuilder
	if builder, err = New(pkg); err != nil {
		return err
	}
	if err = guardValidationMethodRemoval(builder.Scan.Pkg.Dir, args); err != nil {
		return err
	}
	builder.Pretty = args.Pretty
	builder.Validate = args.Validate

	// ValidateJSON validates against the root's generated schema, and a root
	// declared without a schema entrypoint (Declare[T]()) gets none.
	if builder.Validate {
		for _, root := range builder.roots() {
			if root.SchemaMethodName == "" {
				return fmt.Errorf("--validate cannot generate ValidateJSON for %s: it is declared without a schema entrypoint; declare it as polytype.Declare(%s.Schema), or remove --validate", root.Receiver.TypeName, root.Receiver.TypeName)
			}
		}
	}

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

	// Lower, render, and preflight all declaration outputs before any output
	// is mutated. In particular, an unsupported source shape or an unowned
	// output collision must leave ordinary generated artifacts untouched.
	var typeScriptPlan *outputPlan
	var javaScriptPlan *outputPlan
	if args.TypeScriptDir != "" || args.JavaScriptDir != "" {
		definitions, definitionsErr := (&builder).TypeDefinitions()
		if definitionsErr != nil {
			return fmt.Errorf("generate declarations: %w", definitionsErr)
		}
		if args.TypeScriptDir != "" {
			result, generateErr := typescript.Generate(definitions, typescript.Options{Barrel: args.TypeScriptBarrel})
			if generateErr != nil {
				return fmt.Errorf("generate TypeScript output: %w", generateErr)
			}
			typeScriptPlan, err = prepareTypeScriptOutput(args.TypeScriptDir, result.Files, args.TypeScriptBarrel)
			if err != nil {
				return err
			}
		}
		if args.JavaScriptDir != "" {
			result, generateErr := javascript.Generate(definitions, javascript.Options{})
			if generateErr != nil {
				return fmt.Errorf("generate JavaScript output: %w", generateErr)
			}
			javaScriptPlan, err = prepareJavaScriptOutput(args.JavaScriptDir, result.Files)
			if err != nil {
				return err
			}
		}
	}

	var changedSchemas map[string]bool
	var orphanedSchemas []string
	if changedSchemas, orphanedSchemas, err = builder.RenderSchemas(args.NoChanges, args.Force); err != nil {
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
			orphanedMessage := ""
			if len(orphanedSchemas) > 0 {
				orphanedMessage = fmt.Sprintf("; orphaned generated artifacts: %s", strings.Join(orphanedSchemas, ", "))
			}
			return fmt.Errorf("schema changes detected for types: %s%s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(changedTypes, ", "), orphanedMessage)
		}
		if len(orphanedSchemas) > 0 {
			return fmt.Errorf("orphaned generated schema artifacts detected: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(orphanedSchemas, ", "))
		}
		if typeScriptPlan != nil && typeScriptPlan.changed() {
			return fmt.Errorf("TypeScript output changes detected for paths: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(typeScriptPlan.changedPaths(), ", "))
		}
		if javaScriptPlan != nil && javaScriptPlan.changed() {
			return fmt.Errorf("JavaScript output changes detected for paths: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(javaScriptPlan.changedPaths(), ", "))
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
	if javaScriptPlan != nil {
		if err = javaScriptPlan.apply(args.Force); err != nil {
			return err
		}
	}
	return nil
}
