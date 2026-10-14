package grammar_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/goload"
	"github.com/tylergannon/polytype/grammar"
	"github.com/tylergannon/polytype/typescript"
	"golang.org/x/tools/go/packages"
)

func TestSessionLoadsTaggedOverlayDependenciesLazily(t *testing.T) {
	dir := writeFixture(t, map[string]string{"root.go": "package fixture\nimport \"example.com/grammarfixture/dep\"\ntype Root struct { Value dep.Value }\n", "dep/dep.go": "package dep\ninvalid source body\n"})
	cfg := &packages.Config{Dir: dir, Env: append(os.Environ(), "GOWORK=off"), BuildFlags: []string{"-tags=custom"}, Overlay: map[string][]byte{filepath.Join(dir, "dep/dep.go"): []byte("//go:build custom && jsonschema\n\npackage dep\n// Value has a live explanation.\ntype Value struct { Text string }\n")}}
	var session goload.Session
	p, err := grammar.LoadWithSession(cfg, &session, ".")
	require.NoError(t, err)
	require.Equal(t, uint64(1), session.Stats().PackageChecks, "dependencies must not eagerly parse")
	defs, _, err := p.Lower([]grammar.Root{{Type: p.Types().Scope().Lookup("Root").Type()}})
	require.NoError(t, err)
	result, err := typescript.Generate(defs, typescript.Options{})
	require.NoError(t, err)
	var output string
	for _, file := range result.Files {
		output += string(file.Content)
	}
	require.Contains(t, output, "Value has a live explanation.")
	require.Contains(t, output, `"Text": string`)
	before := session.Stats()
	again, err := grammar.LoadWithSession(cfg, &session, ".")
	require.NoError(t, err)
	_, _, err = again.Lower([]grammar.Root{{Type: again.Types().Scope().Lookup("Root").Type()}})
	require.NoError(t, err)
	require.Equal(t, before.PackageChecks, session.Stats().PackageChecks)
	require.Greater(t, session.Stats().PackageHits, before.PackageHits)
	require.Zero(t, session.Stats().FallbackLoads)
}
