package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/goload"
	"github.com/tylergannon/polytype/internal/syntax"
	"golang.org/x/tools/go/packages"
)

// The cache belongs to the declaration CLI and callback-free TypeScript
// configuration path. Loaded-package APIs deliberately bypass it.
const emissionCacheVersion = "polytype-emission-v1"

type cachePackage struct {
	ImportPath, Name, Dir, Export               string
	GoFiles, CompiledGoFiles, Imports, CgoFiles []string
	ImportMap                                   map[string]string
	Module                                      *packages.Module
	Error                                       *struct{ Err string }
}
type cacheInputs struct {
	session        *goload.Session
	overlay        map[string][]byte
	key, path, dir string
	source         syntax.SourceFiles
	packages       map[string]cachePackage
	valid          bool
	mu             sync.Mutex
}
type cacheEnvelope struct {
	Sources     map[string]string
	Key, Digest string
	Plan        json.RawMessage
}

func cacheTrace(event string) {
	if os.Getenv("POLYTYPE_CACHE_TRACE") == "1" {
		fmt.Fprintln(os.Stderr, "polytype cache:", event)
	}
}

func acquireCache(args BuilderArgs) (*cacheInputs, error) { return acquireCacheFor(args, nil) }

func acquireCacheFor(args BuilderArgs, identity any) (*cacheInputs, error) {
	if os.Getenv("POLYTYPE_CACHE") == "off" || len(registeredTransforms) > 0 {
		return nil, fmt.Errorf("disabled or custom transforms")
	}
	cfg := syntax.DefaultPackageCfg
	if cfg.Mode != syntax.PackageLoadNeeds || cfg.Context != nil || cfg.Fset != nil || cfg.Tests || cfg.ParseFile != nil || cfg.Overlay != nil || cfg.Env != nil || cfg.Dir != "" || !reflect.DeepEqual(cfg.BuildFlags, []string{"-tags=jsonschema"}) {
		return nil, fmt.Errorf("custom loader configuration")
	}
	if d := os.Getenv("GOPACKAGESDRIVER"); d != "" && d != "off" {
		return nil, fmt.Errorf("custom packages driver")
	}
	// External overlays/modfiles can contain inputs whose consumed contents this
	// source view cannot attest. Leave their established loader path unchanged.
	for _, flag := range []string{"-overlay", "-modfile", "-toolexec"} {
		if strings.Contains(os.Getenv("GOFLAGS"), flag) {
			return nil, fmt.Errorf("unsupported GOFLAGS %s", flag)
		}
	}
	dir, err := filepath.Abs(args.TargetDir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("cache requires directory target")
	}
	overlay, err := generatedSourceOverlay(dir)
	if err != nil {
		return nil, err
	}
	commandArgs := []string{"list", "-e", "-deps", "-export", "-compiled", "-tags=jsonschema", "-json=ImportPath,Name,Dir,Export,GoFiles,CompiledGoFiles,Imports,CgoFiles,ImportMap,Module,Error"}
	flag, cleanup, err := generatedOverlayFlag(overlay)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if flag != "" {
		commandArgs = append(commandArgs, flag)
	}
	commandArgs = append(commandArgs, ".")
	command := exec.Command("go", commandArgs...)
	command.Dir = dir
	cacheCommand := exec.Command("go", "env", "-json", "GOCACHE", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "GOROOT", "GOVERSION", "GOTOOLDIR", "GOMOD", "GOWORK", "GOTOOLCHAIN", "GOPATH", "GOMODCACHE", "GO111MODULE", "GO386", "GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM")
	cacheCommand.Dir = dir
	// All three inputs are independent. Join the background work before any
	// return, while preserving the serial path's error precedence below.
	var compilerEnvironment []byte
	var environmentErr, binaryErr error
	var binaryDigest [sha256.Size]byte
	var acquisition sync.WaitGroup
	acquisition.Go(func() {
		compilerEnvironment, environmentErr = cacheCommand.Output()
	})
	acquisition.Go(func() {
		var executable string
		executable, binaryErr = os.Executable()
		if binaryErr != nil {
			return
		}
		var binary []byte
		binary, binaryErr = os.ReadFile(executable)
		if binaryErr == nil {
			binaryDigest = sha256.Sum256(binary)
		}
	})
	out, err := command.Output()
	acquisition.Wait()
	if err != nil {
		return nil, err
	}
	if environmentErr != nil {
		return nil, environmentErr
	}
	var effective map[string]string
	if err = json.Unmarshal(compilerEnvironment, &effective); err != nil {
		return nil, err
	}
	compilerCache := effective["GOCACHE"]
	for _, flag := range []string{"-overlay", "-modfile", "-toolexec"} {
		if strings.Contains(effective["GOFLAGS"], flag) {
			return nil, fmt.Errorf("unsupported effective GOFLAGS %s", flag)
		}
	}

	inputs := &cacheInputs{dir: dir, source: syntax.SourceFiles{}, packages: map[string]cachePackage{}, valid: true, overlay: overlay}
	dec := json.NewDecoder(bytes.NewReader(out))
	var graph []cachePackage
	for {
		var p cachePackage
		if err = dec.Decode(&p); err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if p.Error != nil || (filepath.Clean(p.Dir) == dir && len(p.CgoFiles) > 0) {
			return nil, fmt.Errorf("package unavailable or uses cgo: %s", p.ImportPath)
		}
		if p.ImportPath != "unsafe" && !compilerArtifact(p.Export, compilerCache) {
			return nil, fmt.Errorf("non-content-addressed compiler export: %s", p.ImportPath)
		}
		if filepath.Clean(p.Dir) == dir {
			for _, name := range p.GoFiles {
				if filepath.Base(name) == syntax.GeneratedSchemaFile || filepath.Base(name) == syntax.GeneratedCodecFile {
					return nil, fmt.Errorf("generated filename is an authored build input")
				}
			}
		}
		graph = append(graph, p)
		inputs.packages[p.ImportPath] = p
	}
	if len(graph) == 0 {
		return nil, fmt.Errorf("no packages")
	}
	h := sha256.New()
	enc := json.NewEncoder(h)
	_ = enc.Encode(emissionCacheVersion)
	_ = enc.Encode(identity)
	_ = enc.Encode(runtime.Version())
	if binaryErr != nil {
		return nil, binaryErr
	}
	_ = enc.Encode(binaryDigest)

	_ = enc.Encode(effective)
	// Production-file selection intentionally follows this process's build
	// context and raw GOFLAGS, which can differ from Go's persisted GOENV.
	_ = enc.Encode(struct {
		GOOS, GOARCH, GOROOT, Compiler, Flags string
		Cgo                                   bool
		BuildTags, ToolTags, ReleaseTags      []string
	}{build.Default.GOOS, build.Default.GOARCH, build.Default.GOROOT, build.Default.Compiler, os.Getenv("GOFLAGS"), build.Default.CgoEnabled, build.Default.BuildTags, build.Default.ToolTags, build.Default.ReleaseTags})

	// Publication flags are intentionally not semantic inputs. The same result
	// may be checked, forced, or restored into another declaration directory.
	request := struct {
		Dir                              string
		Identity                         any
		Pretty, Validate, TS, Barrel, JS bool
	}{dir, identity, args.Pretty, args.Validate, args.TypeScriptDir != "", args.TypeScriptBarrel, args.JavaScriptDir != ""}
	_ = enc.Encode(request)
	slices.SortFunc(graph, func(a, b cachePackage) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	for _, p := range graph {
		_ = enc.Encode(p)
	}
	if err = inputs.captureDir(dir); err != nil {
		return nil, err
	}
	inputs.key = hex.EncodeToString(h.Sum(nil))
	base := os.Getenv("POLYTYPE_CACHE_DIR")
	if base == "" {
		base, err = os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(base, "polytype")
	}
	// Keep only the latest result for each target and semantic request. Source
	// and executable revisions replace this slot atomically; independent
	// configurations coexist. Output destinations remain publication parameters.
	// The full key remains mandatory inside the envelope.
	slot, _ := json.Marshal(request)
	slotHash := sha256.Sum256(slot)
	inputs.path = filepath.Join(base, emissionCacheVersion, hex.EncodeToString(slotHash[:])+".json")
	return inputs, nil
}

func (c *cacheInputs) load(pattern string) ([]*decorator.Package, error) {
	cacheTrace("load " + pattern)
	cfg := *syntax.DefaultPackageCfg
	cfg.Dir = c.dir
	overlay := c.overlay
	if c.session != nil {
		overlay = nil
	}
	flag, cleanup, err := generatedOverlayFlag(overlay)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if flag != "" {
		cfg.BuildFlags = append(slices.Clone(cfg.BuildFlags), flag)
	}
	if p, ok := c.packages[pattern]; ok {
		if err := c.captureDir(p.Dir); err != nil {
			return nil, err
		}
	}
	cfg.Mode |= packages.NeedExportFile | packages.NeedImports
	cfg.ParseFile = func(fset *token.FileSet, path string, _ []byte) (*ast.File, error) {
		c.mu.Lock()
		content, ok := c.source[filepath.Clean(path)]
		c.mu.Unlock()
		if !ok {
			c.mu.Lock()
			c.valid = false
			c.mu.Unlock()
			var err error
			content, err = os.ReadFile(path)
			if err != nil {
				return nil, err
			}
		}
		return parser.ParseFile(fset, path, content, parser.ParseComments)
	}
	var load goload.LoadFunc
	if c.session != nil {
		ordinary := cfg // retain the established captured parser for fallback
		load = func(request *packages.Config, patterns ...string) ([]*packages.Package, error) {
			return c.session.LoadWithFallback(request, func(_ *packages.Config, patterns ...string) ([]*packages.Package, error) {
				fallback := ordinary
				flag, cleanup, err := generatedOverlayFlag(c.overlay)
				if err != nil {
					return nil, err
				}
				defer cleanup()
				if flag != "" {
					fallback.BuildFlags = append(slices.Clone(fallback.BuildFlags), flag)
				}
				return packages.Load(&fallback, patterns...)
			}, patterns...)
		}
		cfg.ParseFile = nil
		cfg.Overlay = make(map[string][]byte, len(c.source)+len(c.overlay))
		maps.Copy(cfg.Overlay, c.source)
		maps.Copy(cfg.Overlay, c.overlay)
	}
	pkgs, err := syntax.LoadWithLoader(&cfg, load, pattern)
	for _, p := range pkgs {
		if c.session != nil {
			for _, path := range p.CompiledGoFiles {
				if _, ok := cfg.Overlay[filepath.Clean(path)]; !ok {
					c.valid = false
				}
			}
		}
		expected, ok := c.packages[p.PkgPath]
		files := make([]string, len(expected.CompiledGoFiles))
		for i, path := range expected.CompiledGoFiles {
			if !filepath.IsAbs(path) {
				path = filepath.Join(expected.Dir, path)
			}
			files[i] = path
		}
		if !ok || p.ExportFile != expected.Export || !reflect.DeepEqual(p.CompiledGoFiles, files) {
			c.valid = false
		}
	}
	return pkgs, err
}
func (c *cacheInputs) construct(pkg *decorator.Package) (SchemaBuilder, error) {
	scan, err := syntax.LoadPackageWithSource(pkg, c.load, c.source.IsProductionGoFile, c.source)
	if err != nil {
		return SchemaBuilder{}, err
	}
	return newFromScan(scan, nil, true, entrypointSchemas)
}
func (c *cacheInputs) read() (emissionPlan, bool) {
	content, err := os.ReadFile(c.path)
	if err != nil {
		return emissionPlan{}, false
	}
	var envelope cacheEnvelope
	if json.Unmarshal(content, &envelope) != nil || envelope.Key != c.key {
		return emissionPlan{}, false
	}
	if cacheDigest(envelope.Plan, envelope.Sources) != envelope.Digest {
		return emissionPlan{}, false
	}
	if len(envelope.Sources) == 0 {
		return emissionPlan{}, false
	}
	for dir, want := range envelope.Sources {
		if err := c.captureDir(dir); err != nil {
			return emissionPlan{}, false
		}
		if c.sourceDigest(dir) != want {
			return emissionPlan{}, false
		}
	}
	var p emissionPlan
	if json.Unmarshal(envelope.Plan, &p) != nil || filepath.Clean(p.Dir) != c.dir || p.SchemaDir != defaultSubdir || (len(p.Go) == 0 && len(p.TypeScript) == 0) {
		return emissionPlan{}, false
	}
	for _, s := range p.Schemas {
		if filepath.Base(s.Name) != s.Name || s.Name == "." || s.Name == ".." {
			return emissionPlan{}, false
		}
	}
	return p, true
}
func (c *cacheInputs) save(p emissionPlan) {
	if !c.valid {
		cacheTrace("miss (source/compiler view changed; not stored)")
		return
	}
	content, err := json.Marshal(p)
	if err != nil {
		return
	}
	sources := c.sourceManifest()
	envelope, err := json.Marshal(cacheEnvelope{Key: c.key, Digest: cacheDigest(content, sources), Plan: content, Sources: sources})
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.path), 0700) != nil {
		return
	}
	if writeOutputFile(c.path, envelope) == nil {
		cacheTrace("stored")
	}
}

// Source-aware work reaches only a subset of the compiler's package graph.
// Keep the exact source view of those directories in the entry, including
// file membership and build-excluded production files. Untouched dependencies
// contribute only their Go compiler artifact identity to the request key.
func (c *cacheInputs) captureDir(dir string) error {
	dir = filepath.Clean(dir)
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.source {
		if filepath.Dir(path) == dir {
			return nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		c.source[path] = b
	}
	return nil
}
func (c *cacheInputs) sourceDigest(dir string) string {
	paths := []string{}
	for path := range c.source {
		if filepath.Dir(path) != dir {
			continue
		}
		name := filepath.Base(path)
		if dir == c.dir && (name == syntax.GeneratedSchemaFile || name == syntax.GeneratedCodecFile) {
			continue
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, path := range paths {
		_ = enc.Encode(path)
		_ = enc.Encode(c.source[path])
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (c *cacheInputs) sourceManifest() map[string]string {
	dirs := map[string]string{}
	for path := range c.source {
		dirs[filepath.Dir(path)] = ""
	}
	for dir := range dirs {
		dirs[dir] = c.sourceDigest(dir)
	}
	return dirs
}

func compilerArtifact(path, cache string) bool {
	name := filepath.Base(path)
	if len(name) != 66 || !strings.HasSuffix(name, "-d") {
		return false
	}
	if _, err := hex.DecodeString(name[:64]); err != nil {
		return false
	}
	return filepath.Clean(path) == filepath.Join(cache, name[:2], name)
}

func cacheDigest(plan json.RawMessage, sources map[string]string) string {
	content, _ := json.Marshal(struct {
		Plan    json.RawMessage
		Sources map[string]string
	}{plan, sources})
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
