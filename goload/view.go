package goload

import (
	"crypto/sha256"
	"encoding/json"
	"go/parser"
	"go/token"
	"maps"

	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// View owns an explicitly captured private source tree. Its owner guarantees
// that files contains every mutable source and build-control input in that tree,
// and that the physical tree changes only between calls, through Update. External
// versioned modules and the toolchain must remain immutable for this lifetime.
// Ordinary mutable working trees should use Session.Load instead.
//
// A View shares Go discovery for later roots already in a discovered dependency
// closure and amortizes native overlay backing files. Close releases its temporary
// files. Returned package objects remain valid after Close.
type View struct {
	mu            sync.Mutex
	session       *Session
	files         map[string][]byte
	headers       map[string]string
	groups        map[string]*metadataGroup
	temp          string
	closed        bool
	activeSources map[string]bool
	analysis      bool
	logf          func(string, ...any)
}
type metadataGroup struct {
	byPath      map[string]*packages.Package
	byDir       map[string]*packages.Package
	mode        packages.LoadMode
	sources     map[string][]byte
	forceSource map[string]bool
}

// PrepareView creates a lazy view without running Go or reading source files.
// The source map is copied. The owner must call Close when finished and set a
// positive Session.SetMaxUniverses limit for sustained edits. Close releases
// this view's captures; the session owns retained type graphs.
func (s *Session) PrepareView(files map[string][]byte) *View {
	files = cloneOverlay(files)
	return &View{session: s, files: files, headers: sourceHeaders(files), groups: map[string]*metadataGroup{}}
}

// Update replaces the complete captured input snapshot between loads. Ordinary
// Go body and declaration edits preserve discovery. Membership, package/import,
// build/embed directive, and non-Go control changes invalidate discovery.
func (v *View) Update(files map[string][]byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return fmt.Errorf("goload: source view is closed")
	}
	files = cloneOverlay(files)
	if sameFiles(v.files, files) {
		return nil
	}
	if v.temp != "" {
		if err := os.RemoveAll(v.temp); err != nil {
			return err
		}
		v.temp = ""
	}
	headers, changed := v.updatedHeaders(files)
	if changed != "" {
		if v.logf != nil {
			v.logf("goload: source view rediscovery after structural change: %s", changed)
		}
		v.groups = map[string]*metadataGroup{}
	}
	v.files = files
	v.headers = headers
	return nil
}

func (v *View) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	v.groups = nil
	v.files = nil
	v.headers = nil
	if v.temp != "" {
		return os.RemoveAll(v.temp)
	}
	return nil
}
func (v *View) Load(c *packages.Config, patterns ...string) ([]*packages.Package, error) {
	return v.LoadWithFallback(c, packages.Load, patterns...)
}

// LoadWithFallback preserves a consumer acquisition strategy on unsupported
// requests. The callback must not reenter this View; loads and updates are serialized.
func (v *View) LoadWithFallback(c *packages.Config, fallback LoadFunc, patterns ...string) ([]*packages.Package, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return nil, fmt.Errorf("goload: source view is closed")
	}
	config := packages.Config{}
	if c != nil {
		config = *c
		if c.Logf != nil {
			v.logf = c.Logf
		}
	}
	if config.Context != nil {
		if err := config.Context.Err(); err != nil {
			return nil, err
		}
	}
	if config.Mode == 0 {
		config.Mode = packages.LoadFiles
	}
	v.analysis = config.Mode&(packages.NeedTypes|packages.NeedTypesInfo|packages.NeedSyntax) != 0
	config.Overlay = map[string][]byte{}
	for name, b := range v.files {
		if strings.HasSuffix(name, ".go") {
			config.Overlay[name] = b
		}
	}
	if c != nil {
		maps.Copy(config.Overlay, c.Overlay)
	}
	if !supported(&config) {
		return v.session.LoadWithFallback(&config, fallback, patterns...)
	}
	return v.session.load(&config, fallback, v.discover, func() map[string]bool { return v.activeSources }, patterns...)
}
func (v *View) discover(c *packages.Config, patterns ...string) ([]*packages.Package, bool, error) {
	v.activeSources = nil
	if c == nil {
		c = &packages.Config{}
	}
	key, err := v.key(c)
	if err != nil {
		return nil, false, err
	}
	group := v.groups[key]
	if group != nil && c.Mode&^group.mode == 0 {
		roots := make([]*packages.Package, 0, len(patterns))
		for _, pattern := range patterns {
			p := group.byPath[pattern]
			if p == nil && !strings.Contains(pattern, "...") {
				dir := pattern
				if !filepath.IsAbs(dir) {
					dir = filepath.Join(c.Dir, dir)
				}
				dir, _ = filepath.Abs(dir)
				p = group.byDir[filepath.Clean(dir)]
			}
			if p == nil {
				roots = nil
				break
			}
			roots = append(roots, p)
		}
		if len(roots) > 0 {
			group.updateSources(c.Overlay)
			v.activeSources = group.forceSource
			if v.analysis || c.Mode&packages.NeedExportFile == 0 || len(group.forceSource) == 0 {
				return cloneMetadata(roots), true, nil
			}
			// A caller asking for compiler artifacts needs fresh compilation; in-memory
			// source checking intentionally supplies no replacement artifact identity.
			group = nil
		}
	}
	if c.Logf != nil {
		c.Logf("goload: source view metadata miss (known views=%d, mode=%v, query=%v)", len(v.groups), c.Mode, patterns)
	}
	cfg := *c
	if len(c.Overlay) > 0 {
		overlay, e := v.nativeOverlay(c.Overlay)
		if e != nil {
			return nil, false, e
		}
		cfg.Overlay = nil
		cfg.BuildFlags = append(slices.Clone(c.BuildFlags), "-overlay="+overlay)
	}
	roots, err := packages.Load(&cfg, patterns...)
	if err != nil {
		return nil, false, err
	}
	if group == nil || c.Mode&^group.mode != 0 {
		group = &metadataGroup{byPath: map[string]*packages.Package{}, byDir: map[string]*packages.Package{}, mode: c.Mode, sources: cloneOverlay(c.Overlay), forceSource: map[string]bool{}}
		v.groups[key] = group
	}
	seen := map[*packages.Package]bool{}
	var visit func(*packages.Package)
	visit = func(p *packages.Package) {
		if p == nil || seen[p] {
			return
		}
		seen[p] = true
		if len(p.Errors) == 0 {
			group.byPath[p.PkgPath] = p
			if len(p.GoFiles) > 0 {
				group.byDir[filepath.Dir(p.GoFiles[0])] = p
			}
		}
		for _, dep := range p.Imports {
			visit(dep)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	// Dependency metadata has the same target sizes as the discovered root.
	if len(roots) > 0 {
		for _, p := range group.byPath {
			if p.TypesSizes == nil {
				p.TypesSizes = roots[0].TypesSizes
			}
		}
	}
	group.updateSources(c.Overlay)
	v.activeSources = group.forceSource
	return cloneMetadata(roots), false, nil
}
func (v *View) key(c *packages.Config) (string, error) {
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return "", err
	}
	headers := map[string]string{}
	for p, b := range c.Overlay {
		// Capture membership/header changes already clear all metadata groups in
		// Update. Only phase overlays that alter selection belong in this key.
		if original, ok := v.files[p]; ok && slices.Equal(original, b) {
			continue
		}
		header := sourceHeader(p, b)
		if base, ok := v.headers[p]; !ok || base != header {
			headers[p] = header
		}
	}
	data, err := json.Marshal(struct {
		Dir        string
		Env, Flags []string
		Overlay    map[string]string
	}{dir, effectiveEnvironment(env), c.BuildFlags, headers})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
func (v *View) nativeOverlay(files map[string][]byte) (string, error) {
	if v.temp == "" {
		var err error
		v.temp, err = os.MkdirTemp("", "polytype-source-view-")
		if err != nil {
			return "", err
		}
	}
	replace := map[string]string{}
	for name, data := range files {
		sum := sha256.Sum256(data)
		target := filepath.Join(v.temp, fmt.Sprintf("%x", sum))
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if err = os.WriteFile(target, data, 0600); err != nil {
				return "", err
			}
		}
		replace[name] = target
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{replace})
	if err != nil {
		return "", err
	}
	path := filepath.Join(v.temp, fmt.Sprintf("%x.json", sha256.Sum256(data)))
	if _, err = os.Stat(path); os.IsNotExist(err) {
		err = os.WriteFile(path, data, 0600)
	}
	return path, err
}
func sameFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for p, data := range a {
		other, ok := b[p]
		if !ok || !slices.Equal(data, other) {
			return false
		}
	}
	return true
}
func cloneMetadata(roots []*packages.Package) []*packages.Package {
	seen := map[*packages.Package]*packages.Package{}
	var clone func(*packages.Package) *packages.Package
	clone = func(p *packages.Package) *packages.Package {
		if q := seen[p]; q != nil {
			return q
		}
		q := *p
		seen[p] = &q
		q.Imports = map[string]*packages.Package{}
		for path, dep := range p.Imports {
			q.Imports[path] = clone(dep)
		}
		return &q
	}
	out := make([]*packages.Package, len(roots))
	for i, p := range roots {
		out[i] = clone(p)
	}
	return out
}

func sourceHeader(name string, data []byte) string {
	if !strings.HasSuffix(name, ".go") {
		return string(data)
	}
	f, err := parser.ParseFile(token.NewFileSet(), name, data, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return string(data)
	}
	parts := []string{f.Name.Name}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return string(data)
		}
		parts = append(parts, imp.Path.Value)
	}
	// Compiler directives beyond build selection (notably embed and cgo)
	// may depend on declaration placement, so edits to such files rediscover.
	if strings.Contains(string(data), "import \"C\"") {
		return string(data)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "//go:") && !strings.HasPrefix(text, "//go:build") && !strings.HasPrefix(text, "//go:generate") {
			return string(data)
		}
		if strings.HasPrefix(text, "//go:build") || strings.HasPrefix(text, "// +build") {
			// Include the prefix layout so moving a constraint into an invalid position
			// or changing its blank-line separation cannot retain stale selection.
			offset := strings.Index(string(data), line)
			end := strings.IndexByte(string(data)[offset:], '\n')
			if end < 0 {
				end = len(data) - offset
			}
			prefixEnd := max(offset+end, int(f.Package)-1)
			parts = append(parts, string(data[:prefixEnd]))
		}
	}
	return strings.Join(parts, "\x00")
}
func sourceHeaders(files map[string][]byte) map[string]string {
	headers := make(map[string]string, len(files))
	for name, data := range files {
		headers[name] = sourceHeader(name, data)
	}
	return headers
}

func (v *View) updatedHeaders(files map[string][]byte) (map[string]string, string) {
	headers := make(map[string]string, len(files))
	changed := ""
	for name, data := range files {
		if before, ok := v.files[name]; ok && slices.Equal(before, data) {
			headers[name] = v.headers[name]
			continue
		}
		headers[name] = sourceHeader(name, data)
		if before, ok := v.headers[name]; !ok || before != headers[name] {
			changed = name
		}
	}
	for name := range v.files {
		if _, ok := files[name]; !ok {
			changed = name
		}
	}
	return headers, changed
}

func (g *metadataGroup) updateSources(files map[string][]byte) {
	for path, p := range g.byPath {
		for _, name := range p.CompiledGoFiles {
			before, had := g.sources[name]
			after, has := files[name]
			if had != has || !slices.Equal(before, after) {
				g.forceSource[path] = true
				break
			}
		}
	}
	for more := true; more; {
		more = false
		for path, p := range g.byPath {
			if g.forceSource[path] {
				continue
			}
			for _, dep := range p.Imports {
				if g.forceSource[dep.PkgPath] {
					g.forceSource[path] = true
					more = true
					break
				}
			}
		}
	}
	g.sources = cloneOverlay(files)
}
