package goload_test

import (
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tylergannon/polytype/goload"
	"golang.org/x/tools/go/packages"
)

const mode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedExportFile | packages.NeedModule

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.test/session\n\ngo 1.27\n")
	write(t, dir, "common/common.go", "package common\ntype Item struct { Name string }; const Value = 7\n")
	for _, name := range []string{"a", "b"} {
		write(t, dir, name+"/root.go", "package "+name+"\nimport \"example.test/session/common\"\ntype Root struct { Item common.Item }; const Value = common.Value\n")
	}
	return dir
}
func write(t *testing.T, dir, name, value string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(value), 0644); e != nil {
		t.Fatal(e)
	}
}
func load(t *testing.T, s *goload.Session, cfg *packages.Config, pattern string) *packages.Package {
	t.Helper()
	pkgs, e := s.Load(cfg, pattern)
	if e != nil {
		t.Fatal(e)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("packages: %+v", pkgs)
	}
	return pkgs[0]
}
func value(p *packages.Package) int64 {
	n, _ := constant.Int64Val(p.Types.Scope().Lookup("Value").(*types.Const).Val())
	return n
}
func item(p *packages.Package) types.Type {
	return p.Types.Scope().Lookup("Root").Type().Underlying().(*types.Struct).Field(0).Type()
}

func TestDistinctLazyRootsShareTypeIdentityAndCapturedSource(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode, Overlay: map[string][]byte{}}
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(dir, name, "root.go")
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		cfg.Overlay[path] = b
		write(t, dir, name+"/root.go", "this disk content must never be parsed")
	}
	a := load(t, &s, cfg, "./a")
	first := s.Stats()
	if first.PackageChecks != 1 || first.FileParses != 1 || first.FileReads != 0 || first.FallbackLoads != 0 {
		t.Fatalf("first lazy request: %+v", first)
	}
	b := load(t, &s, cfg, "./b")
	if !types.Identical(item(a), item(b)) || item(a) != item(b) {
		t.Fatal("common dependency lost type identity")
	}
	if value(a) != 7 || value(b) != 7 {
		t.Fatal("literal value from captured source missing")
	}
	again := load(t, &s, cfg, "./a")
	if again.Types != a.Types || again.Syntax[0] != a.Syntax[0] {
		t.Fatal("compatible root not reused")
	}
	got := s.Stats()
	if got.PackageChecks != 2 || got.FileParses != 2 || got.FileReads != 0 || got.PackageHits != 1 || got.FallbackLoads != 0 || got.MetadataLoads != 3 {
		t.Fatalf("expected two checks, one hit, no in-process source reads: %+v", got)
	}
	t.Logf("distinct roots + repeat: %+v", got)
}
func TestPromotionStartsSeparateUniverse(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode}
	a := load(t, &s, cfg, "./a")
	imported := a.Imports["example.test/session/common"].Types
	dep := load(t, &s, cfg, "./common")
	if imported == dep.Types {
		t.Fatal("source promotion mutated imported type package")
	}
	if len(dep.Syntax) != 1 || dep.TypesInfo == nil {
		t.Fatal("source promotion missing syntax/info")
	}
	b := load(t, &s, cfg, "./b")
	if item(a) != item(b) {
		t.Fatal("old compatible universe not reusable after promotion")
	}
	if value(b) != 7 {
		t.Fatal("wrong dependency value")
	}
}
func TestChangedSourceOverlayAndBuildTagsIsolate(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode}
	a := load(t, &s, cfg, "./a")
	write(t, dir, "common/common.go", "package common\ntype Item struct { Count int }; const Value = 19\n")
	b := load(t, &s, cfg, "./b")
	if value(b) != 19 || types.Identical(item(a), item(b)) {
		t.Fatal("changed dependency reused stale universe")
	}
	cfg.Overlay = map[string][]byte{filepath.Join(dir, "common/common.go"): []byte("package common\ntype Item struct { Flag bool }; const Value = 31\n")}
	over := load(t, &s, cfg, "./a")
	if value(over) != 31 || types.Identical(item(over), item(b)) {
		t.Fatal("overlay did not isolate")
	}
	cfg.Overlay = nil
	write(t, dir, "common/common.go", "//go:build !alternate\n\npackage common\ntype Item struct { Count int }; const Value = 19\n")
	write(t, dir, "common/tagged.go", "//go:build alternate\n\npackage common\ntype Item struct { Tag string }; const Value = 43\n")
	cfg.BuildFlags = []string{"-tags=alternate"}
	tagged := load(t, &s, cfg, "./a")
	if value(tagged) != 43 {
		t.Fatalf("tag selection gave %d", value(tagged))
	}
	if value(a) != 7 {
		t.Fatal("previous graph mutated")
	}
	if s.Stats().FallbackLoads != 0 {
		t.Fatalf("proof used fallback: %+v", s.Stats())
	}
}
func TestConcurrentIdenticalRequestsReuse(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { load(t, &s, cfg, "./a") })
	}
	wg.Wait()
	if got := s.Stats(); got.PackageChecks != 1 || got.PackageHits != 2 || got.FallbackLoads != 0 {
		t.Fatalf("concurrent work: %+v", got)
	}
}
func TestNewOverlayFile(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode, Overlay: map[string][]byte{filepath.Join(dir, "newdir/new.go"): []byte("package newdir\nconst Value = 53\n")}}
	p := load(t, &s, cfg, "./newdir")
	if value(p) != 53 || s.Stats().FileReads != 0 || s.Stats().FallbackLoads != 0 {
		t.Fatalf("new overlay source not used: %+v", s.Stats())
	}
}

func TestRequestedMetadataSurvivesSourceHit(t *testing.T) {
	dir := fixture(t)
	write(t, dir, "a/root.go", "package a\nimport _ \"embed\"\n//go:embed value.txt\nvar Value string\n")
	write(t, dir, "a/value.txt", "literal data")
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: mode}
	first := load(t, &s, cfg, "./a")
	cfg.Mode |= packages.NeedEmbedFiles | packages.NeedEmbedPatterns
	second := load(t, &s, cfg, "./a")
	if second.Types != first.Types || len(second.EmbedFiles) != 1 || second.EmbedFiles[0] != filepath.Join(dir, "a/value.txt") || len(second.EmbedPatterns) != 1 {
		t.Fatalf("requested embed metadata absent on hit: %+v", second)
	}
}
func TestRecursiveTypeInfoUsesOrdinaryLoader(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps}
	p := load(t, &s, cfg, "./a")
	if p.Imports["example.test/session/common"].TypesInfo == nil || s.Stats().FallbackLoads != 1 || s.Stats().MetadataLoads != 0 {
		t.Fatalf("recursive type info silently dropped: %+v", s.Stats())
	}
}
func TestRecursiveTypesIncludesUnreferencedImports(t *testing.T) {
	dir := fixture(t)
	write(t, dir, "blank/blank.go", "package blank\ntype Only struct{ X int }\n")
	write(t, dir, "common/common.go", "package common\nimport _ \"example.test/session/blank\"\ntype Item struct { Name string }; const Value = 7\n")
	var s goload.Session
	cfg := &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedImports | packages.NeedTypes | packages.NeedDeps}
	p := load(t, &s, cfg, "./a")
	blank := p.Imports["example.test/session/common"].Imports["example.test/session/blank"]
	if blank.Types == nil || !blank.Types.Complete() || blank.Types.Scope().Lookup("Only") == nil || s.Stats().FallbackLoads != 0 {
		t.Fatalf("recursive export types missing: %+v", s.Stats())
	}
}

func TestMetadataOnlyDoesNotCompileBrokenBody(t *testing.T) {
	dir := fixture(t)
	write(t, dir, "a/root.go", "package a\nfunc Broken(){ undefined() }\n")
	var s goload.Session
	p := load(t, &s, &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedFiles}, "./a")
	if p.ExportFile != "" || len(p.Syntax) != 0 || p.Types != nil || p.Name != "a" || s.Stats().PackageChecks != 0 {
		t.Fatalf("metadata request acquired analysis: %+v %+v", p, s.Stats())
	}
}

func TestInvalidSourceKeepsOrdinaryDiagnostics(t *testing.T) {
	dir := fixture(t)
	write(t, dir, "a/root.go", "package a\nvar Value int = \"wrong\"\n")
	cfg := &packages.Config{Dir: dir, Mode: mode}
	want, err := packages.Load(cfg, "./a")
	if err != nil {
		t.Fatal(err)
	}
	var s goload.Session
	got, err := s.Load(cfg, "./a")
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 1 || len(got) != 1 || len(got[0].Errors) == 0 || len(got[0].Errors) != len(want[0].Errors) {
		t.Fatalf("missing diagnostic: %+v", got)
	}
	for i, e := range want[0].Errors {
		if got[0].Errors[i] != e {
			t.Fatalf("diagnostic changed: got %v want %v", got[0].Errors[i], e)
		}
	}
	if s.Stats().FallbackLoads != 1 {
		t.Fatalf("error path did not use ordinary diagnostics: %+v", s.Stats())
	}
}

func TestFallbackUsesConsumerLoaderWithoutHoldingSessionLock(t *testing.T) {
	dir := fixture(t)
	write(t, dir, "common/common.go", "package common\ntype Item Missing; const Value = 7\n")
	path := filepath.Join(dir, "a/root.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &packages.Config{Dir: dir, Mode: mode, Overlay: map[string][]byte{path: content}}
	var s goload.Session
	called := false
	got, err := s.LoadWithFallback(cfg, func(original *packages.Config, patterns ...string) ([]*packages.Package, error) {
		called = true
		if original != cfg || string(original.Overlay[path]) != string(content) || len(patterns) != 1 || patterns[0] != "./a" {
			t.Fatal("fallback configuration changed")
		}
		// Reentering the same session would deadlock if the callback retained its lock.
		// Metadata-only loading also proves the raw typed overlay loader was avoided.
		return s.Load(&packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedFiles}, patterns...)
	}, "./a")
	if err != nil || !called || len(got) != 1 || got[0].Name != "a" || len(got[0].Errors) != 0 {
		t.Fatalf("consumer fallback not used: called=%v err=%v packages=%+v", called, err, got)
	}
	if st := s.Stats(); st.FallbackLoads != 1 || st.PackageChecks != 0 || st.FileParses != 0 {
		t.Fatalf("raw loader escaped callback: %+v", st)
	}
}

func TestRequestedFieldsDoNotMutateSharedAnalysis(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	full := load(t, &s, &packages.Config{Dir: dir, Mode: mode}, "./a")
	limited := load(t, &s, &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedTypes}, "./a")
	if limited.Types != full.Types || limited.Syntax != nil || limited.TypesInfo != nil || limited.GoFiles != nil || limited.CompiledGoFiles != nil || limited.ExportFile != "" || limited.Module != nil || limited.Imports != nil {
		t.Fatalf("unrequested data retained: %+v", limited)
	}
	if len(full.Syntax) != 1 || full.TypesInfo == nil || len(full.Imports) != 1 {
		t.Fatal("pruning changed previously returned graph")
	}
	again := load(t, &s, &packages.Config{Dir: dir, Mode: mode}, "./a")
	if again.Types != full.Types || again.Syntax[0] != full.Syntax[0] || s.Stats().PackageChecks != 1 {
		t.Fatalf("pruning damaged cached full analysis: %+v", s.Stats())
	}
}

func TestUniverseLimitEvictsLeastRecentWithoutChangingReturnedGraphs(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	s.SetMaxUniverses(2)
	path := filepath.Join(dir, "a/root.go")
	config := func(n string) *packages.Config {
		return &packages.Config{Dir: dir, Mode: mode, Overlay: map[string][]byte{path: []byte("package a\nconst Value = " + n + "\n")}}
	}
	one := load(t, &s, config("1"), "./a")
	two := load(t, &s, config("2"), "./a")
	if again := load(t, &s, config("1"), "./a"); again.Types != one.Types {
		t.Fatal("recent compatible graph missed")
	}
	three := load(t, &s, config("3"), "./a")
	if s.Stats().UniverseEvictions != 1 {
		t.Fatalf("unbounded universes: %+v", s.Stats())
	}
	if again := load(t, &s, config("1"), "./a"); again.Types != one.Types {
		t.Fatal("recent graph evicted instead of least recent")
	}
	againTwo := load(t, &s, config("2"), "./a")
	if againTwo.Types == two.Types || s.Stats().PackageChecks != 4 || s.Stats().UniverseEvictions != 2 {
		t.Fatalf("old universe did not reload: %+v", s.Stats())
	}
	if value(one) != 1 || value(two) != 2 || value(three) != 3 || value(againTwo) != 2 {
		t.Fatal("eviction changed previously returned types")
	}
	s.SetMaxUniverses(1)
	if s.Stats().UniverseEvictions != 3 {
		t.Fatalf("smaller bound did not immediately evict: %+v", s.Stats())
	}
	if again := load(t, &s, config("2"), "./a"); again.Types != againTwo.Types {
		t.Fatal("bound change evicted most recent graph")
	}
}
