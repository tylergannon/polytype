package builder

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/tylergannon/polytype/typescript"
)

const (
	typeScriptTypesFile  = "types.ts"
	typeScriptBarrelFile = "index.ts"
)

type plannedTypeScriptFile struct {
	name    string
	content []byte
	changed bool
}

type typescriptOutputPlan struct {
	dir          string
	files        []plannedTypeScriptFile
	removeBarrel bool
}

func prepareTypeScriptOutput(dir string, generated []typescript.File, barrel bool) (*typescriptOutputPlan, error) {
	if dir == "" {
		return nil, fmt.Errorf("TypeScript output directory is empty")
	}
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("TypeScript output path %s is not a directory", dir)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect TypeScript output directory %s: %w", dir, err)
	}

	expected := map[string]bool{typeScriptTypesFile: true}
	if barrel {
		expected[typeScriptBarrelFile] = true
	}
	byName := make(map[string][]byte, len(generated))
	for _, file := range generated {
		if !expected[file.Name] {
			return nil, fmt.Errorf("unexpected TypeScript output filename %q", file.Name)
		}
		if _, duplicate := byName[file.Name]; duplicate {
			return nil, fmt.Errorf("duplicate TypeScript output filename %q", file.Name)
		}
		if !bytes.HasPrefix(file.Content, []byte(typescript.GeneratedHeader)) {
			return nil, fmt.Errorf("generated TypeScript output %s is missing the ownership header", file.Name)
		}
		byName[file.Name] = slices.Clone(file.Content)
	}
	for name := range expected {
		if _, ok := byName[name]; !ok {
			return nil, fmt.Errorf("TypeScript generator did not produce %s", name)
		}
	}

	names := []string{typeScriptTypesFile}
	if barrel {
		names = append(names, typeScriptBarrelFile)
	}
	plan := &typescriptOutputPlan{dir: dir}
	for _, name := range names {
		path := filepath.Join(dir, name)
		existing, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read TypeScript output %s: %w", path, err)
		}
		if err == nil && !bytes.HasPrefix(existing, []byte(typescript.GeneratedHeader)) {
			return nil, fmt.Errorf("refusing to overwrite unowned TypeScript output %s", path)
		}
		plan.files = append(plan.files, plannedTypeScriptFile{
			name:    name,
			content: byName[name],
			changed: errors.Is(err, os.ErrNotExist) || !bytes.Equal(existing, byName[name]),
		})
	}

	if !barrel {
		barrelPath := filepath.Join(dir, typeScriptBarrelFile)
		existing, err := os.ReadFile(barrelPath)
		switch {
		case err == nil && bytes.HasPrefix(existing, []byte(typescript.GeneratedHeader)):
			plan.removeBarrel = true
		case err == nil:
			// An unrelated index.ts is not part of our output set and is preserved.
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, fmt.Errorf("read TypeScript barrel %s: %w", barrelPath, err)
		}
	}

	return plan, nil
}

func (p *typescriptOutputPlan) changed() bool {
	if p.removeBarrel {
		return true
	}
	for _, file := range p.files {
		if file.changed {
			return true
		}
	}
	return false
}

func (p *typescriptOutputPlan) changedPaths() []string {
	paths := make([]string, 0, len(p.files)+1)
	for _, file := range p.files {
		if file.changed {
			paths = append(paths, filepath.Join(p.dir, file.name))
		}
	}
	if p.removeBarrel {
		paths = append(paths, filepath.Join(p.dir, typeScriptBarrelFile))
	}
	slices.Sort(paths)
	return paths
}

func (p *typescriptOutputPlan) apply(force bool) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("create TypeScript output directory %s: %w", p.dir, err)
	}
	for _, file := range p.files {
		path := filepath.Join(p.dir, file.name)
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.HasPrefix(existing, []byte(typescript.GeneratedHeader)) {
				return fmt.Errorf("refusing to overwrite unowned TypeScript output %s", path)
			}
			if bytes.Equal(existing, file.content) && !force {
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read TypeScript output %s before replacement: %w", path, err)
		}
		if err := writeTypeScriptFile(path, file.content); err != nil {
			return err
		}
	}
	if p.removeBarrel {
		path := filepath.Join(p.dir, typeScriptBarrelFile)
		existing, err := os.ReadFile(path)
		if err == nil && !bytes.HasPrefix(existing, []byte(typescript.GeneratedHeader)) {
			return fmt.Errorf("refusing to remove unowned TypeScript barrel %s", path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read TypeScript barrel %s before removal: %w", path, err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove generated TypeScript barrel %s: %w", path, err)
		}
	}
	return nil
}

func writeTypeScriptFile(path string, content []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary TypeScript output for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if closeErr := tmp.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, fmt.Errorf("close temporary TypeScript output for %s: %w", path, closeErr))
		}
		if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary TypeScript output for %s: %w", path, removeErr))
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return fmt.Errorf("write temporary TypeScript output for %s: %w", path, err)
	}
	if err = tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("set permissions on TypeScript output %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temporary TypeScript output for %s: %w", path, err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace TypeScript output %s: %w", path, err)
	}
	return nil
}
