package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylergannon/polytype/internal/testutils"
)

// This is a bounded paired end-to-end measurement on existing cross-package
// union/enum/schema fixtures. Timings are reported, not flaky timing assertions.
// Every run must reproduce independent checked-in goldens.
func TestEmissionCacheRepresentative(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir, cache := t.TempDir(), t.TempDir()
	if err = testutils.CopyDir(filepath.Join(root, "internal/builder/testfixtures"), dir); err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte(strings.Replace(string(mod), "=> ../../..", "=> "+root, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	type metric struct{ wall, cpu time.Duration }
	totals := map[string]metric{}
	for _, target := range []string{"traversal", "crosspkg_union", "interfaces"} {
		packageDir := filepath.Join(dir, target)
		for _, mode := range []string{"off-warmup", "cold", "warm", "off"} {
			cmd := exec.Command(cli, "--target", packageDir, "--pretty")
			cmd.Env = append(os.Environ(), "GOMAXPROCS=2", "POLYTYPE_CACHE_DIR="+cache, "POLYTYPE_CACHE_TRACE=1")
			if mode == "off" || mode == "off-warmup" {
				cmd.Env = append(cmd.Env, "POLYTYPE_CACHE=off")
			}
			start := time.Now()
			out, err := cmd.CombinedOutput()
			wall := time.Since(start)
			if err != nil {
				t.Fatalf("%s %s: %v %s", target, mode, err, out)
			}
			if mode == "warm" && (!bytes.Contains(out, []byte("polytype cache: hit")) || bytes.Contains(out, []byte("polytype cache: load "))) {
				t.Fatalf("warm semantic work: %s", out)
			}
			cpu := cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
			m := totals[mode]
			m.wall += wall
			m.cpu += cpu
			totals[mode] = m
			t.Logf("%s %s wall=%s cpu=%s %s", target, mode, wall, cpu, out)
			count := 0
			err = filepath.WalkDir(packageDir, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".golden") {
					return nil
				}
				expected, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				actual, err := os.ReadFile(strings.TrimSuffix(path, ".golden"))
				if err != nil {
					return err
				}
				if !bytes.Equal(expected, actual) {
					t.Errorf("%s %s output differs from %s", target, mode, path)
				}
				count++
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				t.Fatal("no independent goldens checked")
			}
		}
	}
	for _, mode := range []string{"off", "cold", "warm"} {
		t.Logf("TOTAL %s wall=%s cpu=%s", mode, totals[mode].wall, totals[mode].cpu)
	}
}
