package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/goload"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typescript"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// TypeScriptWorker is the retained implementation of codegen.Worker.
// It deliberately does not persist a hot result under old compiler export IDs.
type WorkerStats struct{ Analyses, Emissions, Replays uint64 }

type TypeScriptWorker struct {
	stats        WorkerStats
	identity     string
	environment  []string
	inputs       *cacheInputs
	roots        map[string]string
	dirs         map[string]bool
	controls     map[string]bool
	snapshot     map[string][]byte
	private      map[string][]byte
	temp, target string
	view         *goload.View
	session      *goload.Session
	plan         *emissionPlan
}

func (w *TypeScriptWorker) Close() error {
	var err error
	if w.view != nil {
		err = w.view.Close()
	}
	if e := os.RemoveAll(w.temp); err == nil {
		err = e
	}
	*w = TypeScriptWorker{stats: w.stats}
	return err
}

func (w *TypeScriptWorker) Run(target string, config ProgrammaticConfig, dir string, barrel, force bool, session *goload.Session) (bool, error) {
	if err := cacheConfigurationSupported(); err != nil {
		_ = w.Close()
		return false, nil
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return true, err
	}
	target = absolute
	for _, u := range config.SealedUnions {
		if u.Inflect != nil {
			return false, nil
		}
	}
	// A function-free configuration has an ordinary deterministic identity.
	identity, err := json.Marshal(struct {
		Target       string
		Declarations []ConfiguredDeclaration
		Unions       [][3]string
		Barrel       bool
	}{Target: target, Declarations: config.Declarations, Unions: workerUnions(config), Barrel: barrel})
	if err != nil {
		return true, err
	}
	env := os.Environ()
	slices.Sort(env)
	if w.inputs != nil && (w.identity != string(identity) || !slices.Equal(w.environment, env) || session != nil && session != w.session) {
		if err = w.Close(); err != nil {
			return true, err
		}
	}
	if dir == "" {
		dir = "."
	}
	args := BuilderArgs{TargetDir: target, TypeScriptDir: dir, TypeScriptBarrel: barrel, Force: force}
	if w.inputs == nil {
		cacheIdentity, _ := typeScriptIdentity(config)
		inputs, e := acquireCacheFor(args, struct{ Worker any }{cacheIdentity})
		if e != nil {
			cacheTrace("worker bypass: " + e.Error())
			return false, nil
		}
		w.inputs = inputs
		w.identity = string(identity)
		w.environment = env
		if err = w.initialize(session); err != nil {
			fallback := w.inputs
			_ = w.Close()
			cacheTrace("worker bypass: " + err.Error())
			return runCachedTypeScriptInputs(fallback, args, config, barrel, session)
		}
	}
	snapshot, err := w.capture()
	if err != nil {
		return true, err
	}
	if w.snapshot != nil && workerStructuralChange(w.snapshot, snapshot) {
		if err = w.Close(); err != nil {
			return true, err
		}
		return w.Run(target, config, dir, barrel, force, session)
	}
	if w.plan != nil && sameWorkerFiles(w.snapshot, snapshot) {
		w.stats.Replays++
		return true, applyConfiguredTypeScript(*w.plan, args)
	}
	if err = w.materialize(snapshot); err != nil {
		return true, err
	}
	first := w.snapshot == nil
	w.snapshot = snapshot
	w.plan = nil
	if w.view == nil {
		w.view = w.session.PrepareView(w.private)
	} else if err = w.view.Update(w.private); err != nil {
		return true, err
	}
	// A disk hit stays before any typed work. Source edits thereafter use the
	// retained immutable view rather than obsolete acquisition-time export IDs.
	if first {
		if plan, hit := w.readPlan(snapshot); hit {
			w.stats.Replays++
			w.plan = &plan
			return true, applyConfiguredTypeScript(plan, args)
		}
	}
	sources := syntax.SourceFiles(maps.Clone(w.private))
	load := func(pattern string) ([]*decorator.Package, error) {
		if p, ok := w.inputs.packages[pattern]; ok && w.rebase(p.Dir) == p.Dir {
			if err := w.inputs.captureDir(p.Dir); err != nil {
				return nil, err
			}
			for name, content := range w.inputs.source {
				if w.rebase(name) == name {
					sources[name] = content
				}
			}
		}
		cfg := *syntax.DefaultPackageCfg
		cfg.Dir = w.target
		cfg.Env = slices.Clone(w.environment)
		// Bind persisted Go settings to the acquisition that owns this capture,
		// rather than letting metadata consult a concurrently edited GOENV file.
		for key, value := range w.inputs.environment {
			switch key {
			case "GOENV", "GOMOD", "GOWORK", "GOVERSION", "GOTOOLDIR":
				continue
			}
			cfg.Env = workerEnv(cfg.Env, key, value)
		}
		cfg.Env = workerEnv(cfg.Env, "GOENV", "off")
		cfg.Env = workerEnv(cfg.Env, "GOWORK", "off")
		cfg.BuildFlags = append(slices.Clone(cfg.BuildFlags), "-trimpath")
		cfg.Mode |= packages.NeedExportFile | packages.NeedImports
		return syntax.LoadWithLoader(&cfg, w.view.Load, pattern)
	}
	w.stats.Analyses++
	pkgs, err := load(".")
	if err != nil {
		return true, w.originalError(err)
	}
	if len(pkgs) == 0 {
		return true, fmt.Errorf("no packages found for %s", target)
	}
	scan, err := syntax.LoadConfiguredPackageWithSource(pkgs[0], load, sources)
	if err != nil {
		return true, w.originalError(err)
	}
	b, err := newProgrammaticFromScan(scan, config, false, false)
	if err != nil {
		return true, w.originalError(err)
	}
	if err = b.ApplyTransforms(); err != nil {
		return true, err
	}
	definitions, _, err := b.ConfiguredTypeDefinitions()
	if err != nil {
		return true, w.originalError(err)
	}
	w.stats.Emissions++
	result, err := typescript.Generate(definitions, typescript.Options{Barrel: barrel})
	if err != nil {
		return true, err
	}
	plan := emissionPlan{Dir: w.inputs.dir, SchemaDir: defaultSubdir, TypeScript: result.Files}
	w.plan = &plan
	if first {
		w.savePlan(snapshot, plan)
	}
	return true, applyConfiguredTypeScript(plan, args)
}

func workerUnions(config ProgrammaticConfig) [][3]string {
	var out [][3]string
	for _, u := range config.SealedUnions {
		out = append(out, [3]string{u.PackagePath, u.TypeName, u.Discriminator})
	}
	return out
}

func (w *TypeScriptWorker) initialize(session *goload.Session) error {
	// A workspace can contribute module requirements from members outside the
	// selected source closure. Keep its established loader until this producer
	// owns a complete workspace acquisition, rather than changing MVS semantics.
	if path := w.inputs.environment["GOWORK"]; path != "" && path != "off" {
		return fmt.Errorf("workspace source capture is not supported")
	}
	w.roots = map[string]string{}
	w.dirs = map[string]bool{}
	w.controls = map[string]bool{}
	var modules []string
	for _, p := range w.inputs.packages {
		m := p.Module
		if m == nil {
			continue
		}
		if m.Replace != nil {
			m = m.Replace
		}
		if m.Version != "" {
			// Only installed versioned modules have the immutable-source lifetime
			// contract; vendored copies and external trees remain live inputs.
			rel, err := filepath.Rel(w.inputs.environment["GOMODCACHE"], p.Dir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("mutable versioned dependency %s", p.ImportPath)
			}
			continue
		}
		if m.Dir == "" {
			return fmt.Errorf("local module has no directory")
		}
		if _, ok := w.roots[m.Dir]; !ok {
			w.roots[m.Dir] = ""
			modules = append(modules, m.Dir)
		}
		w.dirs[p.Dir] = true
		// Native/cgo/embedded build inputs are kept on the established loader path.
		// They require a broader capture than this declaration-only worker owns.
		if len(p.CgoFiles) > 0 || len(p.EmbedPatterns) > 0 {
			return fmt.Errorf("local native or embedded package %s", p.ImportPath)
		}
	}
	if len(modules) == 0 {
		return fmt.Errorf("no local module")
	}
	slices.Sort(modules)
	temp, err := os.MkdirTemp("", "polytype-worker-")
	if err != nil {
		return err
	}
	w.temp = temp
	for i, root := range modules {
		w.roots[root] = filepath.Join(temp, fmt.Sprintf("module-%d", i))
		w.controls[filepath.Join(root, "go.mod")] = true
		w.controls[filepath.Join(root, "go.sum")] = true
	}
	// Local replacement manifests contribute to module selection even when no
	// package from that replacement was imported. Capture and relocate them,
	// without promoting replacements into workspace main modules.
	for i := 0; i < len(modules); i++ {
		root := modules[i]
		path := filepath.Join(root, "go.mod")
		w.controls[path] = true
		w.controls[filepath.Join(root, "go.sum")] = true
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		f, err := modfile.Parse(path, data, nil)
		if err != nil {
			return err
		}
		for _, r := range f.Replace {
			if r.New.Version != "" {
				continue
			}
			dest := r.New.Path
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(root, dest)
			}
			dest = filepath.Clean(dest)
			if _, ok := w.roots[dest]; !ok {
				w.roots[dest] = filepath.Join(temp, fmt.Sprintf("module-%d", len(w.roots)))
				modules = append(modules, dest)
			}
		}
	}
	for _, name := range []string{"GOWORK", "GOENV"} {
		if p := w.inputs.environment[name]; p != "" && p != "off" {
			w.controls[p] = true
			if name == "GOWORK" {
				w.controls[p+".sum"] = true
			}
		}
	}
	w.target = w.rebase(w.inputs.dir)
	if w.target == w.inputs.dir {
		return fmt.Errorf("target outside captured modules")
	}
	w.session = session
	if session == nil {
		w.session = &goload.Session{}
		w.session.SetMaxUniverses(4)
	}
	return nil
}

func (w *TypeScriptWorker) capture() (map[string][]byte, error) {
	files := map[string][]byte{}
	for dir := range w.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(dir, entry.Name())
			// Non-Go compiler inputs invalidate discovery; ordinary output files don't.
			ext := filepath.Ext(name)
			if ext != ".go" && ext != ".s" && ext != ".S" && ext != ".h" && ext != ".c" {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("symlinked mutable source %s", name)
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return nil, err
			}
			files[name] = data
		}
	}
	for name := range w.controls {
		data, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	// Own old generated declarations are never authoritative inputs.
	overlay, err := generatedSourceOverlay(w.inputs.dir)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, overlay)
	return files, nil
}

func (w *TypeScriptWorker) rebase(name string) string {
	var best string
	for root := range w.roots {
		rel, err := filepath.Rel(root, name)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && len(root) > len(best) {
			best = root
		}
	}
	if best == "" {
		return name
	}
	rel, _ := filepath.Rel(best, name)
	return filepath.Join(w.roots[best], rel)
}

func (w *TypeScriptWorker) materialize(files map[string][]byte) error {
	out := map[string][]byte{}
	for name, data := range files {
		path := w.rebase(name)
		if path == name {
			continue
		} // external control is fingerprinted, not executed
		if filepath.Base(path) == "go.mod" {
			f, err := modfile.Parse(name, data, nil)
			if err != nil {
				return err
			}
			for _, r := range f.Replace {
				if r.New.Version != "" {
					continue
				}
				dest := r.New.Path
				if !filepath.IsAbs(dest) {
					dest = filepath.Join(filepath.Dir(name), dest)
				}
				if err = f.AddReplace(r.Old.Path, r.Old.Version, w.rebase(filepath.Clean(dest)), ""); err != nil {
					return err
				}
			}
			data, err = f.Format()
			if err != nil {
				return err
			}
		}
		out[path] = data
	}
	for path := range w.private {
		if _, ok := out[path]; !ok {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	for path, data := range out {
		if bytes.Equal(w.private[path], data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	w.private = out
	return nil
}

func workerEnv(env []string, key, value string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(s string) bool { return strings.HasPrefix(s, key+"=") })
	return append(out, key+"="+value)
}
func sameWorkerFiles(a, b map[string][]byte) bool { return maps.EqualFunc(a, b, bytes.Equal) }
func workerStructuralChange(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return true
	}
	for name, data := range b {
		old, ok := a[name]
		if !ok {
			return true
		}
		if bytes.Equal(old, data) {
			continue
		}
		if !strings.HasSuffix(name, ".go") {
			return true
		}
		header := func(src []byte) string {
			for line := range strings.SplitSeq(string(src), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "//go:") && !strings.HasPrefix(line, "//go:build") && !strings.HasPrefix(line, "//go:generate") {
					return string(src)
				}
			}
			f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.ParseComments)
			if err != nil {
				return string(src)
			}
			var parts []string
			parts = append(parts, f.Name.Name)
			for _, i := range f.Imports {
				parts = append(parts, i.Path.Value)
			}
			for _, g := range f.Comments {
				for _, c := range g.List {
					if strings.HasPrefix(c.Text, "//go:") || strings.HasPrefix(c.Text, "// +build") {
						parts = append(parts, c.Text)
					}
				}
			}
			return strings.Join(parts, "\n")
		}
		if header(old) != header(data) {
			return true
		}
	}
	return false
}
func (w *TypeScriptWorker) originalError(err error) error {
	message := err.Error()
	var roots []string
	for _, root := range w.roots {
		roots = append(roots, root)
	}
	slices.SortFunc(roots, func(a, b string) int { return len(b) - len(a) })
	for _, root := range roots {
		for original, private := range w.roots {
			if private == root {
				message = strings.ReplaceAll(message, private+string(filepath.Separator), original+string(filepath.Separator))
			}
		}
	}
	return fmt.Errorf("%s", message)
}

func (w *TypeScriptWorker) Stats() WorkerStats { return w.stats }

// Reuse the emission envelope and bounded target slot, but bind a Worker entry
// to its complete captured mutable input. Acquisition-time compiler IDs alone
// cannot prove which local bytes the private checker consumed.
func (w *TypeScriptWorker) planKey(snapshot map[string][]byte) string {
	data, _ := json.Marshal(snapshot)
	return fmt.Sprintf("%s:%x", w.inputs.key, sha256.Sum256(data))
}
func (w *TypeScriptWorker) readPlan(snapshot map[string][]byte) (emissionPlan, bool) {
	data, err := os.ReadFile(w.inputs.path)
	if err != nil {
		return emissionPlan{}, false
	}
	var envelope cacheEnvelope
	if json.Unmarshal(data, &envelope) != nil || envelope.Key != w.planKey(snapshot) || cacheDigest(envelope.Plan, nil) != envelope.Digest {
		return emissionPlan{}, false
	}
	var plan emissionPlan
	if json.Unmarshal(envelope.Plan, &plan) != nil || plan.Dir != w.inputs.dir || plan.SchemaDir != defaultSubdir || len(plan.TypeScript) == 0 {
		return emissionPlan{}, false
	}
	return plan, true
}
func (w *TypeScriptWorker) savePlan(snapshot map[string][]byte, plan emissionPlan) {
	data, err := json.Marshal(plan)
	if err != nil {
		return
	}
	envelope, err := json.Marshal(cacheEnvelope{Key: w.planKey(snapshot), Digest: cacheDigest(data, nil), Plan: data})
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(w.inputs.path), 0700) == nil {
		_ = writeOutputFile(w.inputs.path, envelope)
	}
}
