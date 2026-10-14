// Package goload shares compatible Go analysis within an explicitly owned
// invocation. Go remains responsible for file selection and compiler exports.
package goload

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"
)

// Stats counts session work. MetadataLoads includes Go's package discovery and
// export acquisition; FileReads counts only this process's Go source reads.
// FallbackLoads count consumer fallback calls, outside the shared universe.
type Stats struct{ MetadataLoads, PackageChecks, PackageHits, FileReads, FileParses, ExportImports, FallbackLoads, UniverseEvictions, MetadataHits uint64 }

// Session owns immutable analysis results for one invocation or test group.
// Its zero value is ready to use. Shared analysis is serialized and never changes
// process environment. Fallback callbacks may run concurrently. Returned data
// must not be mutated.
// By default a Session retains its graphs until it becomes unreachable. A
// longer-lived owner can bound retained graphs with SetMaxUniverses.
type Session struct {
	mu           sync.Mutex
	stats        Stats
	universes    []*universe
	maxUniverses int
}
type universe struct {
	context      string
	fset         *token.FileSet
	signatures   map[string]string
	types        map[string]*types.Package
	roots        map[string]*packages.Package
	sources      map[string]string
	dependencies map[string][]string
}

// Stats returns a consistent snapshot of the work performed so far.
func (s *Session) Stats() Stats { s.mu.Lock(); defer s.mu.Unlock(); return s.stats }

// SetMaxUniverses bounds the number of compatible type universes retained for
// reuse. A positive limit evicts the least recently used whole universe; zero
// or a negative limit keeps the invocation default of unlimited retention.
// Previously returned packages remain valid after eviction. This bounds retained
// graph versions, not bytes: individual package graphs may vary greatly in size.
// Export-to-source promotion needs another universe; a small limit can therefore
// evict earlier roots while a consumer lazily requests dependency source.
// Eviction never changes source invalidation or combines independent type graphs.
func (s *Session) SetMaxUniverses(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxUniverses = limit
	s.trimUniverses()
}

func (s *Session) trimUniverses() {
	if s.maxUniverses > 0 && len(s.universes) > s.maxUniverses {
		drop := len(s.universes) - s.maxUniverses
		s.stats.UniverseEvictions += uint64(drop)
		clear(s.universes[:drop])
		s.universes = slices.Clone(s.universes[drop:])
	}
}

// Load is like packages.Load, sharing compatible source roots and imported
// types across calls. Overlay bytes are parsed directly, without rereading the
// corresponding source file. Files, membership and compiler export identities
// are checked on every call; changing a fixture never requires manual clearing.
//
// Custom parsers/file sets, tests, external drivers, cgo roots and recursive
// dependency syntax requests use ordinary packages.Load instead. Their results
// are not shared. Importing a package from export data then requesting its source
// starts a separate universe: go/types cannot safely promote an imported package
// into a source-checked package in place.
func (s *Session) Load(config *packages.Config, patterns ...string) ([]*packages.Package, error) {
	return s.LoadWithFallback(config, packages.Load, patterns...)
}

// LoadFunc is a package loader with the standard go/packages signature.
type LoadFunc func(*packages.Config, ...string) ([]*packages.Package, error)

// LoadWithFallback shares supported requests like Load, but delegates requests
// it cannot serve to fallback using the original, unmodified configuration.
// This preserves a consumer's existing overlay/staging or driver strategy.
// The callback runs without the session lock and may make other session calls.
// A nil fallback means packages.Load.
func (s *Session) LoadWithFallback(config *packages.Config, ordinary LoadFunc, patterns ...string) ([]*packages.Package, error) {
	return s.load(config, ordinary, nil, nil, patterns...)
}

type discoveryFunc func(*packages.Config, ...string) ([]*packages.Package, bool, error)

func (s *Session) load(config *packages.Config, ordinary LoadFunc, discover discoveryFunc, sourceOverride func() map[string]bool, patterns ...string) ([]*packages.Package, error) {
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	if ordinary == nil {
		ordinary = packages.Load
	}
	cfg := packages.Config{}
	if config != nil {
		cfg = *config
	}
	metadataLoad := func(c *packages.Config, query ...string) ([]*packages.Package, error) {
		if discover == nil {
			s.stats.MetadataLoads++
			return packages.Load(c, query...)
		}
		p, hit, err := discover(c, query...)
		if hit {
			s.stats.MetadataHits++
		} else {
			s.stats.MetadataLoads++
		}
		return p, err
	}
	fallback := func() ([]*packages.Package, error) {
		s.stats.FallbackLoads++
		locked = false
		s.mu.Unlock()
		return ordinary(config, patterns...)
	}
	if cfg.Mode&(packages.NeedTypes|packages.NeedSyntax|packages.NeedTypesInfo) == 0 {
		p, err := metadataLoad(config, patterns...)
		if discover != nil && err == nil {
			mode := cfg.Mode
			if mode == 0 {
				mode = packages.LoadFiles
			}
			p = requestedFields(p, mode)
		}
		return p, err
	}
	if !supported(&cfg) {
		return fallback()
	}
	cfg.Overlay = cloneOverlay(cfg.Overlay)
	metadata := func() ([]*packages.Package, error) {
		c := cfg
		c.Mode = cfg.Mode&^(packages.NeedTypes|packages.NeedTypesInfo|packages.NeedSyntax) | packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule | packages.NeedExportFile | packages.NeedTypesSizes
		return metadataLoad(&c, patterns...)
	}
	roots, err := metadata()
	if err != nil {
		return nil, err
	}
	// Captured overlays are consumed exactly as supplied. For an ordinary disk
	// request, retain go/packages' standard read-after-discovery behavior instead
	// of imposing an additional compiler discovery pass. Owners needing an exact
	// private snapshot supply captures or use PrepareView.
	for _, p := range roots {
		for _, name := range p.CompiledGoFiles {
			if _, ok := cfg.Overlay[name]; !ok {
				b, e := os.ReadFile(name)
				s.stats.FileReads++
				if e != nil {
					return fallback()
				}
				cfg.Overlay[name] = b
			}
		}
	}
	byPath := map[string]*packages.Package{}
	var visit func(*packages.Package) bool
	visit = func(p *packages.Package) bool {
		if p == nil {
			return false
		}
		if old := byPath[p.PkgPath]; old != nil {
			return old.ID == p.ID
		}
		byPath[p.PkgPath] = p
		for _, dep := range p.Imports {
			if !visit(dep) {
				return false
			}
		}
		return true
	}
	rootSet := map[string]*packages.Package{}
	sourceKeys := map[string]string{}
	for _, p := range roots {
		if !visit(p) || len(p.Errors) > 0 || len(p.CompiledGoFiles) == 0 || p.TypesSizes == nil {
			return fallback()
		}
		for _, name := range p.CompiledGoFiles {
			if !slices.Contains(p.GoFiles, name) {
				return fallback()
			}
		}
		rootSet[p.PkgPath] = p
		h := sha256.New()
		for _, name := range p.CompiledGoFiles {
			_, _ = fmt.Fprintf(h, "%q:%d\x00", name, len(cfg.Overlay[name]))
			h.Write(cfg.Overlay[name])
		}
		sourceKeys[p.PkgPath] = fmt.Sprintf("%x", h.Sum(nil))
	}
	var changedSources map[string]bool
	if sourceOverride != nil {
		changedSources = sourceOverride()
	}
	for path := range changedSources {
		p := byPath[path]
		if p == nil {
			continue
		}
		if len(p.CompiledGoFiles) == 0 {
			return fallback()
		}
		h := sha256.New()
		for _, name := range p.CompiledGoFiles {
			b, ok := cfg.Overlay[name]
			if !ok || !slices.Contains(p.GoFiles, name) {
				return fallback()
			}
			_, _ = fmt.Fprintf(h, "%q:%d\x00", name, len(b))
			h.Write(b)
		}
		rootSet[path] = p
		sourceKeys[path] = fmt.Sprintf("%x", h.Sum(nil))
		p.ExportFile = "" // No compiler artifact exists for this source-checked revision.
	}
	signatures := map[string]string{}
	for path, p := range byPath {
		signatures[path] = signature(p)
		if _, source := rootSet[path]; source {
			// A source-checked package is identified by its captured bytes. Its
			// compiler artifact may change after structural rediscovery even when
			// those bytes did not; imported export-only dependencies still retain
			// their compiler identities below.
			copy := *p
			copy.ExportFile = ""
			signatures[path] = signature(&copy) + ":source:" + sourceKeys[path]
		}
		if _, root := rootSet[path]; !root && path != "unsafe" && (p.ExportFile == "" || len(p.Errors) > 0) {
			if cfg.Logf != nil {
				cfg.Logf("goload: ordinary fallback for dependency %s (export=%q, errors=%v)", path, p.ExportFile, p.Errors)
			}
			return fallback()
		}
	}
	context := contextKey(&cfg, roots)
	var u *universe
	for i, candidate := range s.universes {
		if candidate.context != context {
			continue
		}
		compatible := true
		for path, key := range signatures {
			if old, ok := candidate.signatures[path]; ok && old != key {
				compatible = false
				break
			}
		}
		for path, key := range sourceKeys {
			if old, ok := candidate.sources[path]; ok && old != key {
				compatible = false
				break
			}
			if candidate.types[path] != nil && candidate.roots[path] == nil {
				compatible = false
				break
			}
		}
		if compatible {
			u = candidate
			copy(s.universes[i:], s.universes[i+1:])
			s.universes[len(s.universes)-1] = candidate
			break
		}
	}
	if u == nil && discover != nil {
		for _, candidate := range slices.Backward(s.universes) {

			if candidate.context == context {
				u = deriveUniverse(candidate, signatures, sourceKeys)
				break
			}
		}
	}
	if u != nil && !slices.Contains(s.universes, u) {
		s.universes = append(s.universes, u)
		s.trimUniverses()
	}
	if u == nil {
		u = &universe{context: context, fset: token.NewFileSet(), signatures: map[string]string{}, types: map[string]*types.Package{"unsafe": types.Unsafe}, roots: map[string]*packages.Package{}, sources: map[string]string{}, dependencies: map[string][]string{}}
		s.universes = append(s.universes, u)
		s.trimUniverses()
	}
	for path, key := range signatures {
		u.signatures[path] = key
		u.dependencies[path] = nil
		for _, dep := range byPath[path].Imports {
			u.dependencies[path] = append(u.dependencies[path], dep.PkgPath)
		}
	}
	loading := map[string]bool{}
	var check func(*packages.Package) (*packages.Package, error)
	check = func(p *packages.Package) (*packages.Package, error) {
		if old := u.roots[p.PkgPath]; old != nil {
			s.stats.PackageHits++
			p.Types, p.TypesInfo, p.Syntax, p.Fset = old.Types, old.TypesInfo, old.Syntax, old.Fset
			p.Errors, p.IllTyped = slices.Clone(old.Errors), old.IllTyped
			return p, nil
		}
		if loading[p.PkgPath] {
			return nil, fmt.Errorf("import cycle at %s", p.PkgPath)
		}
		loading[p.PkgPath] = true
		defer delete(loading, p.PkgPath)
		p.Fset = u.fset
		p.Types = types.NewPackage(p.PkgPath, p.Name)
		p.TypesInfo = newTypesInfo()
		failed := func(err error, kind packages.ErrorKind) (*packages.Package, error) {
			if discover == nil {
				return nil, err
			}
			// The captured view already owns discovery. A bad ordinary edit is a
			// fresh source diagnostic, not a reason to invoke another compiler.
			p.Errors = append(p.Errors, packages.Error{Msg: err.Error(), Kind: kind})
			p.IllTyped = true
			u.roots[p.PkgPath] = p
			u.sources[p.PkgPath] = sourceKeys[p.PkgPath]
			return p, nil
		}
		for _, name := range p.CompiledGoFiles {
			f, e := parser.ParseFile(u.fset, name, cfg.Overlay[name], parser.ParseComments|parser.AllErrors)
			s.stats.FileParses++
			if f != nil {
				p.Syntax = append(p.Syntax, f)
			}
			if e != nil {
				return failed(e, packages.ParseError)
			}
		}
		importer := importFunc(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			dep := p.Imports[path]
			if dep == nil {
				return nil, fmt.Errorf("missing selected import %q", path)
			}
			if source := rootSet[dep.PkgPath]; source != nil {
				got, e := check(source)
				if e != nil {
					return nil, e
				}
				if got.IllTyped {
					return nil, fmt.Errorf("package %s has invalid source: %v", path, got.Errors)
				}
				return got.Types, nil
			}
			return s.importExport(u, dep)
		})
		tc := types.Config{Importer: importer, Sizes: p.TypesSizes}
		if p.Module != nil && p.Module.GoVersion != "" {
			tc.GoVersion = "go" + p.Module.GoVersion
		}
		s.stats.PackageChecks++
		if e := types.NewChecker(&tc, u.fset, p.Types, p.TypesInfo).Files(p.Syntax); e != nil {
			return failed(e, packages.TypeError)
		}
		u.types[p.PkgPath] = p.Types
		u.roots[p.PkgPath] = p
		u.sources[p.PkgPath] = sourceKeys[p.PkgPath]
		return p, nil
	}
	out := make([]*packages.Package, 0, len(roots))
	for _, p := range roots {
		got, e := check(p)
		if e != nil {
			return fallback()
		}
		out = append(out, got)
	}
	if cfg.Mode&packages.NeedDeps != 0 && cfg.Mode&packages.NeedTypes != 0 {
		for path, p := range byPath {
			if path == "unsafe" {
				u.types[path] = types.Unsafe
				continue
			}
			if source := rootSet[path]; source != nil && source.IllTyped {
				continue // Retain current source diagnostics, never reload stale exports.
			}
			if _, err := s.importExport(u, p); err != nil {
				return fallback()
			}
		}
	}
	// Populate the standard metadata graph with shared imported type objects.
	for path, p := range byPath {
		if tp := u.types[path]; tp != nil {
			p.Types = tp
			p.Fset = u.fset
		}
	}
	return requestedFields(out, cfg.Mode), nil
}

// Return fresh metadata wrappers so mode pruning never changes cached analysis
// or a graph previously returned to another consumer.
func requestedFields(roots []*packages.Package, mode packages.LoadMode) []*packages.Package {
	copies := map[*packages.Package]*packages.Package{}
	var copyPackage func(*packages.Package) *packages.Package
	copyPackage = func(p *packages.Package) *packages.Package {
		if old := copies[p]; old != nil {
			return old
		}
		q := *p
		copies[p] = &q
		if mode&packages.NeedImports != 0 {
			q.Imports = map[string]*packages.Package{}
			for path, dep := range p.Imports {
				q.Imports[path] = copyPackage(dep)
			}
		} else {
			q.Imports = nil
		}
		if mode&packages.NeedName == 0 {
			q.Name = ""
			q.PkgPath = ""
		}
		if mode&packages.NeedFiles == 0 {
			q.GoFiles = nil
			q.OtherFiles = nil
			q.IgnoredFiles = nil
		}
		if mode&packages.NeedCompiledGoFiles == 0 {
			q.CompiledGoFiles = nil
		}
		if mode&packages.NeedExportFile == 0 {
			q.ExportFile = ""
		}
		if mode&packages.NeedTypes == 0 {
			q.Types = nil
			q.IllTyped = false
		}
		if mode&packages.NeedSyntax == 0 {
			q.Syntax = nil
		}
		if mode&packages.NeedTypesInfo == 0 {
			q.TypesInfo = nil
		}
		if mode&packages.NeedTypesSizes == 0 {
			q.TypesSizes = nil
		}
		if mode&packages.NeedModule == 0 {
			q.Module = nil
		}
		if mode&packages.NeedEmbedFiles == 0 {
			q.EmbedFiles = nil
		}
		if mode&packages.NeedEmbedPatterns == 0 {
			q.EmbedPatterns = nil
		}
		return &q
	}
	out := make([]*packages.Package, len(roots))
	for i, p := range roots {
		out[i] = copyPackage(p)
	}
	return out
}

func (s *Session) importExport(u *universe, p *packages.Package) (*types.Package, error) {
	if tp := u.types[p.PkgPath]; tp != nil && tp.Complete() {
		return tp, nil
	}
	f, err := os.Open(p.ExportFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// The pinned Go 1.27 toolchain emits compiler archives. NewReader supports
	// those archives; unsupported future export formats take the ordinary loader
	// fallback rather than introducing a second archive implementation.
	//lint:ignore SA1019 The pinned Go 1.27 compiler emits archives; unsupported formats use the ordinary loader.
	r, err := gcexportdata.NewReader(f) //nolint:staticcheck // Compiler archives remain the supported Go 1.27 export format.
	if err != nil {
		return nil, err
	}
	s.stats.ExportImports++
	return gcexportdata.Read(r, u.fset, u.types, p.PkgPath)
}

type importFunc func(string) (*types.Package, error)

func (f importFunc) Import(path string) (*types.Package, error) { return f(path) }
func newTypesInfo() *types.Info {
	return &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{}, Instances: map[*ast.Ident]types.Instance{}, Scopes: map[ast.Node]*types.Scope{}, Selections: map[*ast.SelectorExpr]*types.Selection{}, FileVersions: map[*ast.File]string{}}
}
func cloneOverlay(in map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range in {
		out[filepath.Clean(k)] = slices.Clone(v)
	}
	return out
}
func supported(c *packages.Config) bool {
	if c.ParseFile != nil || c.Fset != nil || c.Tests || c.Mode&(packages.NeedSyntax|packages.NeedTypesInfo) != 0 && c.Mode&packages.NeedDeps != 0 {
		return false
	}
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	driverOff := false
	for _, entry := range env {
		if entry == "GOPACKAGESDRIVER=off" {
			driverOff = true
		}
	}
	if !driverOff {
		if _, err := exec.LookPath("gopackagesdriver"); err == nil {
			return false
		}
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOPACKAGESDRIVER=") && entry != "GOPACKAGESDRIVER=" && entry != "GOPACKAGESDRIVER=off" {
			return false
		}
		if strings.HasPrefix(entry, "GOFLAGS=") && strings.Contains(entry, "-overlay") {
			return false
		}
	}
	for _, flag := range c.BuildFlags {
		if strings.HasPrefix(flag, "-overlay") {
			return false
		}
	}
	return true
}
func signature(p *packages.Package) string {
	b, _ := json.Marshal(struct {
		ID, Export string
		Files      []string
		Module     *packages.Module
	}{p.ID, p.ExportFile, p.CompiledGoFiles, p.Module})
	return string(b)
}
func contextKey(c *packages.Config, roots []*packages.Package) string {
	// Keep build views separate even if a changed tag happens not to affect this
	// first root. File paths and export signatures handle distinct modules.
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	var selected []string
	for _, entry := range effectiveEnvironment(env) {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GO") || strings.HasPrefix(key, "CGO") || key == "CC" || key == "CXX" || key == "PATH" {
			selected = append(selected, entry)
		}
	}
	slices.Sort(selected)
	var sizes []string
	for _, p := range roots {
		if p.TypesSizes != nil {
			sizes = append(sizes, fmt.Sprintf("%d/%d", p.TypesSizes.Sizeof(types.Typ[types.Int]), p.TypesSizes.Alignof(types.Typ[types.Int64])))
		}
	}
	slices.Sort(sizes)
	sizes = slices.Compact(sizes)
	b, _ := json.Marshal([]any{c.BuildFlags, selected, sizes})
	return string(b)
}

// Derive a coherent graph by retaining only objects whose entire dependency
// closure is unchanged. Old packages remain immutable and keep their old graph.
func deriveUniverse(old *universe, signatures, sources map[string]string) *universe {
	invalid := map[string]bool{}
	for path, key := range signatures {
		if before, ok := old.signatures[path]; ok && before != key {
			invalid[path] = true
		}
	}
	for path, key := range sources {
		if before, ok := old.sources[path]; ok && before != key {
			invalid[path] = true
		}
		if old.types[path] != nil && old.roots[path] == nil {
			invalid[path] = true
		}
	}
	for more := true; more; {
		more = false
		for path, deps := range old.dependencies {
			if invalid[path] {
				continue
			}
			for _, dep := range deps {
				if invalid[dep] {
					invalid[path] = true
					more = true
					break
				}
			}
		}
	}
	// Retain positions for unchanged objects without retaining every historical
	// edited file in a shared FileSet. Fresh wrappers below refer only to this set.
	removed := map[*token.File]bool{}
	for path, p := range old.roots {
		if invalid[path] {
			for _, f := range p.Syntax {
				removed[old.fset.File(f.Pos())] = true
			}
		}
	}
	fset := token.NewFileSet()
	old.fset.Iterate(func(f *token.File) bool {
		if !removed[f] {
			fset.AddExistingFiles(f)
		}
		return true
	})
	u := &universe{context: old.context, fset: fset, signatures: map[string]string{}, types: map[string]*types.Package{}, roots: map[string]*packages.Package{}, sources: map[string]string{}, dependencies: map[string][]string{}}
	for p, v := range old.signatures {
		if !invalid[p] {
			u.signatures[p] = v
		}
	}
	for p, v := range old.types {
		if !invalid[p] {
			u.types[p] = v
		}
	}
	for p, v := range old.roots {
		if !invalid[p] {
			copy := *v
			copy.Fset = fset
			u.roots[p] = &copy
		}
	}
	for p, v := range old.sources {
		if !invalid[p] {
			u.sources[p] = v
		}
	}
	for p, v := range old.dependencies {
		if !invalid[p] {
			u.dependencies[p] = v
		}
	}
	return u
}

func effectiveEnvironment(env []string) []string {
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	slices.Sort(out)
	return out
}
