package grammar

import (
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// LoadMode is the information FromPackages needs for each package whose source
// is lowered. Add packages.NeedDeps to obtain that information for imports, or
// select the necessary dependency packages as explicit packages.Load patterns.
const LoadMode = syntax.PackageLoadNeeds | packages.NeedImports

// FromPackages lowers from an already parsed and type-checked package snapshot.
// It preserves root's type identity and uses its Imports graph plus additional
// packages to resolve source. It never invokes a loader or the Go command; a
// reached package without source produces an error identifying what is missing.
// Packages used only through built-in grammar support (such as time.Time) do
// not need source.
//
// config must describe the load that produced the snapshot and explicitly set
// the jsonschema tag in BuildFlags. Unlike LoadWithConfig, this constructor
// cannot add a tag to an already loaded package. Call packages.Load with at
// least LoadMode and a parser that retains comments (the default does).
// All packages must belong to the same type-checking operation, with identical
// build settings, module selection and overlays. packages.Package does not
// retain those settings: their correspondence is the caller's responsibility.
//
// The caller owns the snapshot and must keep its ASTs, FileSet, Types, TypesInfo,
// source files and overlay resources alive and unchanged until all Lower calls
// finish. FromPackages does not mutate them. Do not share a Package between
// concurrent Lower calls, or reuse a snapshot after editing source; load a fresh
// snapshot for the next generation. No state is retained globally.
func FromPackages(config *packages.Config, root *packages.Package, additional ...*packages.Package) (*Package, error) {
	if config == nil || !hasJSONSchemaTag(config.BuildFlags) {
		return nil, fmt.Errorf("FromPackages requires a snapshot loaded with an explicit jsonschema build tag in Config.BuildFlags")
	}
	if root == nil {
		return nil, fmt.Errorf("FromPackages: nil root package")
	}
	byPath := map[string]*packages.Package{}
	visited := map[*packages.Package]bool{}
	var visit func(*packages.Package) error
	visit = func(pkg *packages.Package) error {
		if pkg == nil {
			return fmt.Errorf("FromPackages: nil package in snapshot")
		}
		if visited[pkg] {
			return nil
		}
		visited[pkg] = true
		if previous := byPath[pkg.PkgPath]; previous != nil && previous != pkg {
			return fmt.Errorf("FromPackages: conflicting snapshots for package %s; supply packages from one load", pkg.PkgPath)
		}
		byPath[pkg.PkgPath] = pkg
		for _, imported := range pkg.Imports {
			if err := visit(imported); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	for _, pkg := range additional {
		if err := visit(pkg); err != nil {
			return nil, err
		}
	}

	decorated := map[string]*decorator.Package{}
	// Marker admission uses the loaded AST's build constraints, including
	// overlay-only files, rather than reopening a different source view.
	headers := map[string]string{}
	production := productionContext(config)
	production.OpenFile = func(filename string) (io.ReadCloser, error) {
		header, ok := headers[filename]
		if !ok {
			return nil, fmt.Errorf("FromPackages: source header %s is missing from the snapshot", filename)
		}
		return io.NopCloser(strings.NewReader(header)), nil
	}
	loader := func(path string) ([]*decorator.Package, error) {
		if pkg := decorated[path]; pkg != nil {
			return []*decorator.Package{pkg}, nil
		}
		pkg := byPath[path]
		if pkg == nil {
			return nil, fmt.Errorf("FromPackages: package %s is missing from the supplied snapshot", path)
		}
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("package %s has errors: %s", path, pkg.Errors[0])
		}
		if pkg.IllTyped || pkg.Types == nil || pkg.TypesInfo == nil || pkg.TypesInfo.Uses == nil || pkg.Fset == nil || len(pkg.Syntax) == 0 || len(pkg.GoFiles) == 0 {
			return nil, fmt.Errorf("FromPackages: package %s needs type-checked source; load it with grammar.LoadMode (and NeedDeps for imports)", path)
		}
		converted := &decorator.Package{Package: pkg, Decorator: decorator.NewDecoratorFromPackage(pkg)}
		goFiles := make(map[string]bool, len(pkg.GoFiles))
		for _, filename := range pkg.GoFiles {
			goFiles[filename] = true
		}
		for _, file := range pkg.Syntax {
			if file == nil || pkg.Fset.File(file.Pos()) == nil {
				return nil, fmt.Errorf("FromPackages: package %s has syntax outside its FileSet", path)
			}
			filename := pkg.Fset.File(file.Pos()).Name()
			var header strings.Builder
			for _, group := range file.Comments {
				if group.Pos() >= file.Package {
					break
				}
				for _, comment := range group.List {
					header.WriteString(comment.Text + "\n")
				}
				header.WriteByte('\n')
			}
			header.WriteString("package " + file.Name.Name + "\n")
			headers[filename] = header.String()
			// Match decorator.Load: preprocessed cgo files are not ordinary Go
			// source and cannot be decorated.
			if !goFiles[filename] {
				continue
			}
			convertedFile, err := converted.Decorator.DecorateFile(file)
			if err != nil {
				return nil, fmt.Errorf("decorate package %s: %w", path, err)
			}
			converted.Syntax = append(converted.Syntax, convertedFile)
			if converted.Dir == "" {
				converted.Dir = filepath.Dir(filename)
			}
		}
		if len(converted.Syntax) == 0 {
			return nil, fmt.Errorf("FromPackages: package %s has no ordinary Go source in the supplied snapshot", path)
		}
		decorated[path] = converted
		return []*decorator.Package{converted}, nil
	}
	pkgs, err := loader(root.PkgPath)
	if err != nil {
		return nil, err
	}
	b, err := builder.NewForLoadWithLoader(pkgs[0], loader, func(filename string) (bool, error) {
		return production.MatchFile(filepath.Dir(filename), filepath.Base(filename))
	})
	if err != nil {
		return nil, err
	}
	return &Package{builder: b}, nil
}

func hasJSONSchemaTag(flags []string) bool {
	return slices.Contains(snapshotTags(flags), syntax.BuildTag)
}

func snapshotTags(flags []string) []string {
	var tags string
	for i := 0; i < len(flags); i++ {
		if flags[i] == "-tags" && i+1 < len(flags) {
			i++
			tags = flags[i]
		} else if value, ok := strings.CutPrefix(flags[i], "-tags="); ok {
			tags = value
		}
	}
	return strings.FieldsFunc(tags, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
}

func productionContext(config *packages.Config) build.Context {
	ctx := build.Default
	ctx.BuildTags = nil
	for _, tag := range snapshotTags(config.BuildFlags) {
		if tag != syntax.BuildTag {
			ctx.BuildTags = append(ctx.BuildTags, tag)
		}
	}
	env := config.Env
	if env == nil {
		env = os.Environ()
	}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		switch key {
		case "GOOS":
			ctx.GOOS = value
		case "GOARCH":
			ctx.GOARCH = value
		case "CGO_ENABLED":
			ctx.CgoEnabled = value == "1"
		}
	}
	return ctx
}
