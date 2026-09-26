package builder

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// plannedOutputFile is one file a declaration backend wants to write.
type plannedOutputFile struct {
	name    string
	content []byte
	changed bool
}

// outputPlan is the preflighted, not-yet-applied set of writes and removals
// for one declaration output directory. Building a plan never mutates the
// directory; apply performs the mutations after every earlier generation step
// has succeeded.
//
// Ownership is decided by the generated-file header AND the fixed Polytype
// file names (types.ts, index.ts, types.js). A file in the desired set whose
// current content lacks the header is refused, never overwritten. A file in
// obsoleteCandidates that carries the header is removed; one without it is an
// application file and is left alone.
type outputPlan struct {
	dir    string
	label  string
	header string
	files  []plannedOutputFile
	remove []string
}

// prepareOutputPlan validates the generated files and inspects the directory
// without writing anything. desired must name exactly the expected files (in
// any order); obsoleteCandidates names files that are no longer produced but
// are Polytype-owned by header and must be removed.
func prepareOutputPlan(dir, label, header string, desired []plannedOutputFile, expected []string, obsoleteCandidates []string) (*outputPlan, error) {
	if dir == "" {
		return nil, fmt.Errorf("%s output directory is empty", label)
	}
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("%s output path %s is not a directory", label, dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect %s output directory %s: %w", label, dir, err)
	}

	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	byName := make(map[string][]byte, len(desired))
	for _, file := range desired {
		if !expectedSet[file.name] {
			return nil, fmt.Errorf("unexpected %s output filename %q", label, file.name)
		}
		if _, duplicate := byName[file.name]; duplicate {
			return nil, fmt.Errorf("duplicate %s output filename %q", label, file.name)
		}
		if !bytes.HasPrefix(file.content, []byte(header)) {
			return nil, fmt.Errorf("generated %s output %s is missing the ownership header", label, file.name)
		}
		byName[file.name] = slices.Clone(file.content)
	}
	for _, name := range expected {
		if _, ok := byName[name]; !ok {
			return nil, fmt.Errorf("%s generator did not produce %s", label, name)
		}
	}

	plan := &outputPlan{dir: dir, label: label, header: header}
	for _, name := range expected {
		path := filepath.Join(dir, name)
		existing, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s output %s: %w", label, path, err)
		}
		if err == nil && !bytes.HasPrefix(existing, []byte(header)) {
			return nil, fmt.Errorf("refusing to overwrite unowned %s output %s", label, path)
		}
		plan.files = append(plan.files, plannedOutputFile{
			name:    name,
			content: byName[name],
			changed: errors.Is(err, os.ErrNotExist) || !bytes.Equal(existing, byName[name]),
		})
	}

	for _, name := range obsoleteCandidates {
		if expectedSet[name] {
			continue
		}
		path := filepath.Join(dir, name)
		existing, err := os.ReadFile(path)
		switch {
		case err == nil && bytes.HasPrefix(existing, []byte(header)):
			plan.remove = append(plan.remove, name)
		case err == nil:
			// An application-owned file outside our output set is preserved.
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, fmt.Errorf("read %s output %s: %w", label, path, err)
		}
	}

	return plan, nil
}

func (p *outputPlan) changed() bool {
	if len(p.remove) > 0 {
		return true
	}
	for _, file := range p.files {
		if file.changed {
			return true
		}
	}
	return false
}

func (p *outputPlan) changedPaths() []string {
	paths := make([]string, 0, len(p.files)+len(p.remove))
	for _, file := range p.files {
		if file.changed {
			paths = append(paths, filepath.Join(p.dir, file.name))
		}
	}
	for _, name := range p.remove {
		paths = append(paths, filepath.Join(p.dir, name))
	}
	slices.Sort(paths)
	return paths
}

func (p *outputPlan) apply(force bool) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("create %s output directory %s: %w", p.label, p.dir, err)
	}
	for _, file := range p.files {
		path := filepath.Join(p.dir, file.name)
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.HasPrefix(existing, []byte(p.header)) {
				return fmt.Errorf("refusing to overwrite unowned %s output %s", p.label, path)
			}
			if bytes.Equal(existing, file.content) && !force {
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s output %s before replacement: %w", p.label, path, err)
		}
		if err := writeOutputFile(path, file.content); err != nil {
			return err
		}
	}
	for _, name := range p.remove {
		path := filepath.Join(p.dir, name)
		existing, err := os.ReadFile(path)
		if err == nil && !bytes.HasPrefix(existing, []byte(p.header)) {
			return fmt.Errorf("refusing to remove unowned %s output %s", p.label, path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s output %s before removal: %w", p.label, path, err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove generated %s output %s: %w", p.label, path, err)
		}
	}
	return nil
}

func writeOutputFile(path string, content []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if closeErr := tmp.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, fmt.Errorf("close temporary output for %s: %w", path, closeErr))
		}
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary output for %s: %w", path, removeErr))
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return fmt.Errorf("write temporary output for %s: %w", path, err)
	}
	if err = tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("set permissions on output %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temporary output for %s: %w", path, err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace output %s: %w", path, err)
	}
	return nil
}
