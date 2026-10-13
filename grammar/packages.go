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
// config must describe the load that produced the snapshot and set the
// jsonschema tag in BuildFlags or GOFLAGS (Config.Env, or the process environment
// when Env is nil). Unlike LoadWithConfig, this constructor
// cannot add a tag to an already loaded package. Call packages.Load with at
// least LoadMode, Tests: false, and a parser that retains comments (the default does).
// All packages must belong to the same type-checking operation, with identical
// build settings, module selection and overlays. packages.Package does not
// retain those settings: their correspondence is the caller's responsibility.
// Generation follows Go's flag precedence: an explicit -tags replaces GOFLAGS
// tags rather than combining with them.
//
// The caller owns the snapshot and must keep its ASTs, FileSet, Types, TypesInfo,
// source files and overlay resources alive and unchanged until all Lower calls
// finish. FromPackages does not mutate them. Do not share a Package between
// concurrent Lower calls, or reuse a snapshot after editing source; load a fresh
// snapshot for the next generation. No state is retained globally.
//
// Production declaration admission removes jsonschema from explicit build tags;
// if that was the only explicit tag, production falls back to GOFLAGS tags.
// The jsonschema tag is removed from that production fallback as well.
// String-mode enum adaptation also retains the existing on-disk inspection of
// production JSON methods, including files excluded by jsonschema. That check
// does not use overlays; keep the production source view available on disk.
func FromPackages(config *packages.Config, root *packages.Package, additional ...*packages.Package) (*Package, error) {
	if config == nil {
		return nil, fmt.Errorf("FromPackages requires the config used to load the snapshot")
	}
	envFlags, err := snapshotEnvFlags(config)
	if err != nil {
		return nil, err
	}
	if !hasJSONSchemaTag(append(envFlags, config.BuildFlags...)) {
		return nil, fmt.Errorf("FromPackages requires a snapshot loaded with an explicit jsonschema build tag in Config.BuildFlags or GOFLAGS")
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
	view := newSourceView(config, envFlags)
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
		if err := view.add(pkg); err != nil {
			return nil, err
		}
		converted := &decorator.Package{Package: pkg, Decorator: decorator.NewDecoratorFromPackage(pkg)}
		goFiles := make(map[string]bool, len(pkg.GoFiles))
		for _, filename := range pkg.GoFiles {
			goFiles[filename] = true
		}
		for _, file := range pkg.Syntax {
			filename := pkg.Fset.File(file.Pos()).Name()
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
	b, err := builder.NewForLoadWithLoader(pkgs[0], loader, view.isProduction)
	if err != nil {
		return nil, err
	}
	return &Package{builder: b}, nil
}

// sourceView gives both loading entry points the same AST-backed declaration
// admission, including overlay-only files and the caller's production tags.
type sourceView struct {
	headers    map[string]string
	production build.Context
}

func newSourceView(config *packages.Config, envFlags []string) *sourceView {
	view := &sourceView{headers: map[string]string{}, production: productionContext(config, envFlags)}
	view.production.OpenFile = func(filename string) (io.ReadCloser, error) {
		header, ok := view.headers[filename]
		if !ok {
			return nil, fmt.Errorf("source header %s is missing from the package view", filename)
		}
		return io.NopCloser(strings.NewReader(header)), nil
	}
	return view
}

func (v *sourceView) add(pkg *packages.Package) error {
	for _, file := range pkg.Syntax {
		if file == nil || pkg.Fset == nil || pkg.Fset.File(file.Pos()) == nil {
			return fmt.Errorf("package %s has syntax outside its FileSet", pkg.PkgPath)
		}
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
		v.headers[pkg.Fset.File(file.Pos()).Name()] = header.String()
	}
	return nil
}

func (v *sourceView) isProduction(filename string) (bool, error) {
	return v.production.MatchFile(filepath.Dir(filename), filepath.Base(filename))
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

func productionContext(config *packages.Config, envFlags []string) build.Context {
	ctx := build.Default
	withoutGenerationTag := func(tags []string) []string {
		return slices.DeleteFunc(tags, func(tag string) bool { return tag == syntax.BuildTag })
	}
	ctx.BuildTags = withoutGenerationTag(snapshotTags(config.BuildFlags))
	if len(ctx.BuildTags) == 0 {
		// A generation-only -tags=jsonschema switch disappears in production;
		// GOFLAGS still applies to that production invocation.
		ctx.BuildTags = withoutGenerationTag(snapshotTags(envFlags))
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

func snapshotEnvFlags(config *packages.Config) ([]string, error) {
	env := config.Env
	if env == nil {
		env = os.Environ()
	}
	var value string
	for _, entry := range env {
		if flags, ok := strings.CutPrefix(entry, "GOFLAGS="); ok {
			value = flags
		}
	}
	// GOFLAGS uses Go's quoted-field convention, not shell escaping: only a
	// quote at the beginning of a field has meaning, and contents are literal.
	var flags []string
	for value = strings.TrimLeft(value, " \t\r\n"); value != ""; value = strings.TrimLeft(value, " \t\r\n") {
		end := strings.IndexAny(value, " \t\r\n")
		if value[0] == '\'' || value[0] == '"' {
			quote := value[0]
			value = value[1:]
			end = strings.IndexByte(value, quote)
			if end < 0 {
				return nil, fmt.Errorf("FromPackages: unterminated quote in GOFLAGS")
			}
			flags = append(flags, value[:end])
			value = value[end+1:]
			continue
		}
		if end < 0 {
			end = len(value)
		}
		flags = append(flags, value[:end])
		value = value[end:]
	}
	return flags, nil
}
