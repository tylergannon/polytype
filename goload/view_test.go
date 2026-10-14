package goload_test

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tylergannon/polytype/goload"
	"golang.org/x/tools/go/packages"
)

func captureFixture(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil {
			out[path] = b
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPreparedViewReusesDiscoveredClosureWithoutGo(t *testing.T) {
	dir := fixture(t)
	var s goload.Session
	view := s.PrepareView(captureFixture(t, dir))
	t.Cleanup(func() {
		if err := view.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	a, err := view.Load(cfg, "./a")
	if err != nil || len(a) != 1 || value(a[0]) != 7 {
		t.Fatalf("initial load: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	common, err := view.Load(cfg, "./common")
	if err != nil || len(common) != 1 || value(common[0]) != 7 {
		t.Fatalf("cached closure requires Go: %v", err)
	}
	again, err := view.Load(cfg, "./common")
	if err != nil || again[0].Types != common[0].Types {
		t.Fatalf("source reuse: %v", err)
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.MetadataHits != 2 || st.PackageChecks != 2 || st.FileReads != 0 || st.FallbackLoads != 0 {
		t.Fatalf("unexpected work: %+v", st)
	}
}

func TestPreparedViewBodyAndDependencyAPIEditWithoutGo(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	view := s.PrepareView(files)
	defer func() { _ = view.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	initial, err := view.Load(cfg, "./a", "./b")
	if err != nil {
		t.Fatal(err)
	}
	initialImports := s.Stats().ExportImports
	t.Setenv("PATH", t.TempDir())
	aFile := filepath.Join(dir, "a/root.go")
	files[aFile] = append(files[aFile], []byte("\nfunc body() int { const local = 23; return local }\n")...)
	if err := view.Update(files); err != nil {
		t.Fatal(err)
	}
	body, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatalf("body edit ran Go: %v", err)
	}
	found := false
	for n, obj := range body[0].TypesInfo.Defs {
		if n.Name == "local" {
			c, ok := obj.(*types.Const)
			found = ok && c.Val().ExactString() == "23"
		}
	}
	if !found || item(body[0]) != item(initial[0]) || s.Stats().ExportImports != initialImports {
		t.Fatal("body edit did not retain exact unchanged import objects")
	}
	commonFile := filepath.Join(dir, "common/common.go")
	files[commonFile] = []byte("package common\ntype Item struct { Name int }; const Value = 29\n")
	if err := view.Update(files); err != nil {
		t.Fatal(err)
	}
	a, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatalf("dependency edit ran Go: %v", err)
	}
	b, err := view.Load(cfg, "./b")
	if err != nil {
		t.Fatalf("reverse importer edit ran Go: %v", err)
	}
	if value(a[0]) != 29 || value(b[0]) != 29 || value(initial[0]) != 7 || value(initial[1]) != 7 {
		t.Fatal("dependency edit stale or old graphs mutated")
	}
	if item(a[0]) != item(b[0]) || item(a[0]) == item(initial[0]) {
		t.Fatal("changed dependency type identity is incoherent")
	}
	field := item(a[0]).Underlying().(*types.Struct).Field(0)
	if field.Type() != types.Typ[types.Int] {
		t.Fatalf("dependency API remained stale: %v", field.Type())
	}
	if a[0].ExportFile != "" || a[0].Imports["example.test/session/common"].ExportFile != "" {
		t.Fatal("source-checked revision advertised stale compiler artifact")
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.MetadataHits != 3 || st.FallbackLoads != 0 || st.FileReads != 0 || st.PackageChecks != 6 {
		t.Fatalf("unexpected hot work: %+v", st)
	}
}

func TestPreparedViewStructuralChangesRequireGo(t *testing.T) {
	for _, change := range []string{"import", "build", "module", "membership"} {
		t.Run(change, func(t *testing.T) {
			dir := fixture(t)
			files := captureFixture(t, dir)
			var s goload.Session
			view := s.PrepareView(files)
			defer func() { _ = view.Close() }()
			cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
			if _, err := view.Load(cfg, "./a"); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", t.TempDir())
			path := filepath.Join(dir, "a/root.go")
			switch change {
			case "import":
				files[path] = []byte("package a\nimport \"fmt\"\nvar Value=fmt.Sprint(1)\n")
			case "build":
				files[path] = append([]byte("//go:build custom\n\n"), files[path]...)
			case "module":
				files[filepath.Join(dir, "go.mod")] = []byte("module example.test/other\n\ngo 1.27\n")
			case "membership":
				files[filepath.Join(dir, "a/new.go")] = []byte("package a\nconst New=1\n")
			}
			if err := view.Update(files); err != nil {
				t.Fatal(err)
			}
			if _, err := view.Load(cfg, "./a"); err == nil {
				t.Fatal("structural change reused metadata despite unavailable Go")
			}
			if s.Stats().MetadataLoads != 2 || s.Stats().MetadataHits != 0 {
				t.Fatalf("structural change bypassed discovery: %+v", s.Stats())
			}
		})
	}
}

func TestPreparedViewCanonicalEnvironmentAndBoundedPositions(t *testing.T) {
	dir := fixture(t)
	files := captureFixture(t, dir)
	var s goload.Session
	s.SetMaxUniverses(2)
	view := s.PrepareView(files)
	defer func() { _ = view.Close() }()
	cfg := &packages.Config{Dir: dir, Mode: mode, Env: append(os.Environ(), "GOPACKAGESDRIVER=off")}
	first, err := view.Load(cfg, "./a")
	if err != nil {
		t.Fatal(err)
	}
	countFiles := func(p *packages.Package) int {
		n := 0
		p.Fset.Iterate(func(*token.File) bool { n++; return true })
		return n
	}
	initialFiles := countFiles(first[0])
	slices.Reverse(cfg.Env)
	t.Setenv("PATH", t.TempDir())
	again, err := view.Load(cfg, "./a")
	if err != nil || again[0].Types != first[0].Types {
		t.Fatalf("environment order invalidated view: %v", err)
	}
	file := filepath.Join(dir, "a/root.go")
	base := slices.Clone(files[file])
	for i := 0; i < 12; i++ {
		files[file] = append(slices.Clone(base), []byte(fmt.Sprintf("\nconst Edit = %d\n", i))...)
		if err := view.Update(files); err != nil {
			t.Fatal(err)
		}
		got, err := view.Load(cfg, "./a")
		if err != nil {
			t.Fatal(err)
		}
		if n := countFiles(got[0]); n != initialFiles {
			t.Fatalf("retained historical token files at edit%d: %d want%d", i, n, initialFiles)
		}
	}
	if st := s.Stats(); st.MetadataLoads != 1 || st.PackageChecks != 13 || st.UniverseEvictions != 11 {
		t.Fatalf("unbounded/reloaded edit history: %+v", st)
	}
	if value(first[0]) != 7 || countFiles(first[0]) != initialFiles {
		t.Fatal("old returned graph mutated")
	}
}
