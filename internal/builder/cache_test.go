package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tylergannon/polytype/goload"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "polytype-builder-cache")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("POLYTYPE_CACHE_DIR", dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCacheBindsCapturedSourceAndActualCompilerIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("POLYTYPE_CACHE_DIR", t.TempDir())
	for name, source := range map[string]string{"go.mod": "module example.com/capture\n\ngo 1.27\n", "types.go": "package capture\ntype Value struct { Before string }\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := acquireCache(BuilderArgs{TargetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// This edit changes the Go compiler's artifact. Parsing still must consume
	// the captured bytes, while the entry must not be stored with the old ID.
	if err = os.WriteFile(filepath.Join(dir, "types.go"), []byte("package capture\ntype Value struct { After int }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pkgs, err := inputs.load(".")
	if err != nil {
		t.Fatal(err)
	}
	actual := pkgs[0].Types.Scope().Lookup("Value").Type().Underlying().String()
	if !strings.Contains(actual, "Before string") || strings.Contains(actual, "After") {
		t.Fatalf("parser did not consume captured source: %s", actual)
	}
	if inputs.valid {
		t.Fatal("changed compiler view remained cacheable")
	}
	inputs.save(emissionPlan{Dir: dir, SchemaDir: defaultSubdir, Go: []byte("package capture\n")})
	if _, err = os.Stat(inputs.path); !os.IsNotExist(err) {
		t.Fatalf("mixed view stored: %v", err)
	}
}

func TestConcurrentEmissionCacheWrites(t *testing.T) {
	dir := t.TempDir()
	inputs := &cacheInputs{key: "test", path: filepath.Join(dir, "cache", "entry.json"), dir: dir, valid: true, source: map[string][]byte{filepath.Join(dir, "types.go"): []byte("package fixture\n")}}
	plan := emissionPlan{Dir: dir, SchemaDir: defaultSubdir, Go: []byte("package fixture\n")}
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() { inputs.save(plan) })
	}
	writers.Wait()
	got, hit := inputs.read()
	if !hit || string(got.Go) != "package fixture\n" {
		t.Fatalf("concurrent writes produced invalid entry: hit=%v %#v", hit, got)
	}
	content, err := os.ReadFile(inputs.path)
	if err != nil {
		t.Fatal(err)
	}
	// Syntactically valid corruption of the source manifest must fail its digest.
	content = []byte(strings.Replace(string(content), `"Sources":`, `"Sources":{"/corrupted":"bad"},"IgnoredSources":`, 1))
	if err = os.WriteFile(inputs.path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if _, hit = inputs.read(); hit {
		t.Fatal("corrupt source manifest was trusted")
	}
}

func TestCompilerArtifactIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "go-build")
	name := strings.Repeat("ab", 32) + "-d"
	if !compilerArtifact(filepath.Join(root, "ab", name), root) {
		t.Fatal("Go content-addressed artifact rejected")
	}
	for _, path := range []string{filepath.Join(root, "stable.a"), filepath.Join(root, "cd", name), filepath.Join(t.TempDir(), "ab", name)} {
		if compilerArtifact(path, root) {
			t.Fatalf("mutable or foreign export accepted: %s", path)
		}
	}
}

func TestSessionCacheDeclinesUncapturedParsedSource(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"go.mod": "module example.com/capture\n\ngo 1.27\n", "types.go": "package capture\ntype Value struct { Before string }\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := acquireCache(BuilderArgs{TargetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	inputs.session = &goload.Session{}
	delete(inputs.source, filepath.Join(dir, "types.go"))
	pkgs, err := inputs.load(".")
	if err != nil {
		t.Fatal(err)
	}
	actual := pkgs[0].Types.Scope().Lookup("Value").Type().Underlying().String()
	if !strings.Contains(actual, "Before string") {
		t.Fatalf("missing literal type: %s", actual)
	}
	if inputs.valid || inputs.session.Stats().FileReads != 1 {
		t.Fatalf("uncaptured session parse accepted for persistent cache: valid=%v stats=%+v", inputs.valid, inputs.session.Stats())
	}
}
