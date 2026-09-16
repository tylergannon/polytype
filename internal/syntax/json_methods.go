package syntax

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Names of the Go file generated into a package: GeneratedSchemaFile when it
// embeds JSON Schema, otherwise GeneratedCodecFile. A package holds at most
// one of them.
const (
	GeneratedSchemaFile = "jsonschema_gen.go"
	GeneratedCodecFile  = "polytype_gen.go"
)

// JSONMethod identifies a handwritten production JSON codec method. Generated
// output and generation-only declaration stubs are deliberately excluded.
type JSONMethod struct {
	Receiver string
	Name     string
	Position token.Position
}

// FindProductionJSONMethods finds active production MarshalJSON and
// UnmarshalJSON declarations for the requested local receiver types.
func FindProductionJSONMethods(dir string, receivers []string) ([]JSONMethod, error) {
	wanted := make(map[string]bool, len(receivers))
	for _, receiver := range receivers {
		wanted[receiver] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var methods []JSONMethod
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == GeneratedSchemaFile || name == GeneratedCodecFile {
			continue
		}
		matches, err := IsProductionGoFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if !matches {
			continue
		}
		fileMethods, err := findJSONMethodsInFile(fset, filepath.Join(dir, name), wanted)
		if err != nil {
			return nil, err
		}
		methods = append(methods, fileMethods...)
	}
	return methods, nil
}

// FindGeneratedJSONMethods finds generated MarshalJSON and UnmarshalJSON
// declarations for the requested receiver types. The generated file is
// inspected directly because it is excluded while the package is loaded with
// the reserved jsonschema build tag.
func FindGeneratedJSONMethods(dir string, receivers []string) ([]JSONMethod, error) {
	wanted := make(map[string]bool, len(receivers))
	for _, receiver := range receivers {
		wanted[receiver] = true
	}
	var methods []JSONMethod
	for _, name := range []string{GeneratedSchemaFile, GeneratedCodecFile} {
		filename := filepath.Join(dir, name)
		if _, err := os.Stat(filename); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		fileMethods, err := findJSONMethodsInFile(token.NewFileSet(), filename, wanted)
		if err != nil {
			return nil, err
		}
		methods = append(methods, fileMethods...)
	}
	return methods, nil
}

func findJSONMethodsInFile(fset *token.FileSet, filename string, wanted map[string]bool) ([]JSONMethod, error) {
	file, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, err
	}
	var methods []JSONMethod
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !slices.Contains([]string{"MarshalJSON", "UnmarshalJSON"}, fn.Name.Name) {
			continue
		}
		receiver, ok := receiverTypeName(fn.Recv.List[0].Type)
		if !ok || (len(wanted) > 0 && !wanted[receiver]) {
			continue
		}
		methods = append(methods, JSONMethod{
			Receiver: receiver,
			Name:     fn.Name.Name,
			Position: fset.Position(fn.Pos()),
		})
	}
	return methods, nil
}

// IsProductionGoFile reports whether filename is active for the current
// production build. GOFLAGS tags are included so collision discovery follows
// the same custom-tag environment as the Go command that invoked generation,
// except the generation tag itself: a production build never sets it, even
// when GOFLAGS does for an editor or for generation.
func IsProductionGoFile(filename string) (bool, error) {
	context := build.Default
	fields := strings.Fields(os.Getenv("GOFLAGS"))
	for i := 0; i < len(fields); i++ {
		value := ""
		switch {
		case strings.HasPrefix(fields[i], "-tags="):
			value = strings.TrimPrefix(fields[i], "-tags=")
		case fields[i] == "-tags" && i+1 < len(fields):
			i++
			value = fields[i]
		}
		value = strings.Trim(value, `"'`)
		if value != "" {
			for _, tag := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
				if tag != BuildTag {
					context.BuildTags = append(context.BuildTags, tag)
				}
			}
		}
	}
	return context.MatchFile(filepath.Dir(filename), filepath.Base(filename))
}

func receiverTypeName(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name, true
	case *ast.StarExpr:
		return receiverTypeName(value.X)
	case *ast.ParenExpr:
		return receiverTypeName(value.X)
	case *ast.IndexExpr:
		return receiverTypeName(value.X)
	case *ast.IndexListExpr:
		return receiverTypeName(value.X)
	default:
		return "", false
	}
}
