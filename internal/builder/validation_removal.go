package builder

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// guardValidationMethodRemoval keeps an omitted generation flag from silently
// deleting methods that callers may already depend on. The guard runs before
// any schema or Go output is written. Passing Force is the explicit request to
// accept the breaking removal.
func guardValidationMethodRemoval(targetDir string, args BuilderArgs) error {
	if args.Force {
		return nil
	}

	path := filepath.Join(targetDir, "jsonschema_gen.go")
	source, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing generated code for validation methods: %w", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("inspect existing generated code for validation methods: %w", err)
	}
	if !ast.IsGenerated(file) {
		return nil
	}

	desired := map[string]bool{
		"ValidateJSON": args.Validate,
	}
	removed := make(map[string]bool)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil {
			continue
		}
		if keep, tracked := desired[function.Name.Name]; tracked && !keep {
			removed[function.Name.Name] = true
		}
	}
	if len(removed) == 0 {
		return nil
	}

	methods := make([]string, 0, len(removed))
	for method := range removed {
		methods = append(methods, method)
	}
	slices.Sort(methods)

	return fmt.Errorf(
		"refusing to remove previously generated %s from jsonschema_gen.go; rerun with %s to preserve validation, or pass --force to remove it intentionally",
		strings.Join(methods, " and "),
		"--validate",
	)
}
