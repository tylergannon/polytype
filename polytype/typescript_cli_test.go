package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestGenCommandTypeScriptOutput drives the built CLI against a fresh consumer
// module outside the repository. It covers the three CLI behaviors the retired
// tests/typescript/check.mjs lane asserted and no Go test did: path resolution
// of a relative --typescript directory, --no-changes detection of both a stale
// types.ts and a missing requested barrel without mutating the output, and
// agreement between the JSON Schema enum membership and the generated
// TypeScript literal unions.
func TestGenCommandTypeScriptOutput(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs("..")
	require.NoError(t, err)

	// The CLI is invoked from the parent of the target so that the relative
	// --typescript path can be proven to resolve against the working
	// directory rather than against --target.
	root := t.TempDir()
	consumer := filepath.Join(root, "consumer")
	require.NoError(t, os.MkdirAll(consumer, 0o755))
	require.NoError(t, testutils.CopyDir("testdata/consumer", consumer))
	require.NoError(t, os.WriteFile(filepath.Join(consumer, "go.mod"), []byte(fmt.Sprintf(
		"module example.com/ts-consumer\n\ngo %s\n\nrequire github.com/tylergannon/polytype v0.0.0\n\nreplace github.com/tylergannon/polytype => %s\n",
		goDirective(t, repoRoot), repoRoot)), 0o644))
	runGo(t, consumer, "mod", "tidy")

	cli := filepath.Join(root, "polytype")
	cwd, err := os.Getwd()
	require.NoError(t, err)
	runGo(t, cwd, "build", "-o", cli, ".")

	generate := func(t *testing.T, args ...string) (int, string) {
		t.Helper()
		exit, stdout, stderr, err := testutils.RunCommand(cli, root, append([]string{"gen", "--target", consumer}, args...)...)
		require.NoError(t, err)
		return exit, stdout + stderr
	}

	generated := filepath.Join(consumer, "generated")
	exit, output := generate(t, "--typescript", "consumer/generated", "--typescript-barrel")
	require.Equal(t, 0, exit, output)
	require.Equal(t, []string{"index.ts", "types.ts"}, names(t, generated),
		"a relative --typescript path resolves against the working directory, not --target")
	require.Contains(t, string(read(t, filepath.Join(generated, "index.ts"))), "export type")

	// The schema and the declarations must agree on enum membership, values
	// included: the wire form of Priority is its evaluated integer, and
	// PriorityName's is the constant name.
	var envelope struct {
		Properties struct {
			Status       struct{ Enum []string } `json:"status"`
			Priority     struct{ Enum []int }    `json:"priority"`
			PriorityName struct{ Enum []string } `json:"priority_name"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(read(t, filepath.Join(consumer, "jsonschema", "Envelope.json")), &envelope))
	require.Equal(t, []string{"ready", `wait"ing`, "converted"}, envelope.Properties.Status.Enum)
	require.Equal(t, []int{0, 1, 8, 4}, envelope.Properties.Priority.Enum)
	require.Equal(t, []string{"Low", "High", "Urgent", "Medium"}, envelope.Properties.PriorityName.Enum)

	types := string(read(t, filepath.Join(generated, "types.ts")))
	require.Contains(t, types, `export type Status = "ready" | "wait\"ing" | "converted";`)
	require.Contains(t, types, "export type Priority = 0 | 1 | 8 | 4;")

	current := snapshot(t, generated)

	// A stale types.ts fails --no-changes and is left exactly as it was.
	typesPath := filepath.Join(generated, "types.ts")
	require.NoError(t, os.WriteFile(typesPath, append(read(t, typesPath), []byte("// deliberately stale\n")...), 0o644))
	stale := snapshot(t, generated)
	exit, output = generate(t, "--typescript", generated, "--typescript-barrel", "--no-changes")
	require.NotEqual(t, 0, exit, "a stale types.ts must fail --no-changes")
	require.Regexp(t, `(?i)typescript|types\.ts`, output)
	require.Equal(t, stale, snapshot(t, generated), "--no-changes must not rewrite the stale output")

	exit, output = generate(t, "--typescript", generated, "--typescript-barrel")
	require.Equal(t, 0, exit, output)
	require.Equal(t, current, snapshot(t, generated))

	// A missing barrel is equally a change: generating without the flag
	// removes index.ts, after which --no-changes with the flag must fail.
	exit, output = generate(t, "--typescript", generated)
	require.Equal(t, 0, exit, output)
	require.Equal(t, []string{"types.ts"}, names(t, generated))
	withoutBarrel := snapshot(t, generated)
	exit, output = generate(t, "--typescript", generated, "--typescript-barrel", "--no-changes")
	require.NotEqual(t, 0, exit, "a missing requested barrel must fail --no-changes:\n%s", output)
	require.Equal(t, withoutBarrel, snapshot(t, generated), "--no-changes must not create the missing barrel")
}

func goDirective(t *testing.T, repoRoot string) string {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", repoRoot, "list", "-m", "-f", "{{.GoVersion}}")
	require.NoError(t, err)
	require.Equal(t, 0, exit, stderr)
	return string(stdout[:len(stdout)-1])
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	exit, stdout, stderr, err := testutils.RunCommand("go", dir, args...)
	require.NoError(t, err)
	require.Equal(t, 0, exit, "go %v:\n%s\n%s", args, stdout, stderr)
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return contents
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var found []string
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	sort.Strings(found)
	return found
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	digests := map[string]string{}
	for _, name := range names(t, dir) {
		digests[name] = fmt.Sprintf("%x", sha256.Sum256(read(t, filepath.Join(dir, name))))
	}
	return digests
}
