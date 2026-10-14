package builder

import (
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/javascript"
	"github.com/tylergannon/polytype/typescript"
)

// emissionPlan is Polytype's serializable result of discovery, lowering and
// projection. It contains no Go AST/type pointers or remembered output state.
// Publication always checks today's output ownership and removal guards.
type emissionPlan struct {
	Dir             string
	SchemaDir       string
	GenerateSchemas bool
	Go              []byte
	Schemas         []schemaEmission
	TypeScript      []typescript.File
	JavaScript      []javascript.File
}
type schemaEmission struct {
	Type, Name string
	Content    []byte
}

func (p emissionPlan) apply(args BuilderArgs) error {
	if err := guardValidationMethodRemoval(p.Dir, args); err != nil {
		return err
	}
	var ts, js *outputPlan
	var err error
	if args.TypeScriptDir != "" {
		ts, err = prepareTypeScriptOutput(args.TypeScriptDir, p.TypeScript, args.TypeScriptBarrel)
		if err != nil {
			return err
		}
	}
	if args.JavaScriptDir != "" {
		js, err = prepareJavaScriptOutput(args.JavaScriptDir, p.JavaScript)
		if err != nil {
			return err
		}
	}
	name, other := syntax.GeneratedCodecFile, syntax.GeneratedSchemaFile
	if p.GenerateSchemas {
		name, other = other, name
	}
	changed, orphans, err := p.applySchemas(args.NoChanges, args.Force)
	if err != nil {
		return err
	}
	if args.NoChanges {
		if len(changed) > 0 {
			slices.Sort(changed)
			extra := ""
			if len(orphans) > 0 {
				extra = fmt.Sprintf("; orphaned generated artifacts: %s", strings.Join(orphans, ", "))
			}
			return fmt.Errorf("schema changes detected for types: %s%s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(changed, ", "), extra)
		}
		if len(orphans) > 0 {
			return fmt.Errorf("orphaned generated schema artifacts detected: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(orphans, ", "))
		}
		if ts != nil && ts.changed() {
			return fmt.Errorf("TypeScript output changes detected for paths: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(ts.changedPaths(), ", "))
		}
		if js != nil && js.changed() {
			return fmt.Errorf("JavaScript output changes detected for paths: %s (and --no-changes or JSONSCHEMA_NO_CHANGES was set)", strings.Join(js.changedPaths(), ", "))
		}
	}
	otherPath := filepath.Join(p.Dir, other)
	stale, err := ownedGeneratedFile(otherPath)
	if err != nil {
		return err
	}
	if err = writeOutputFile(filepath.Join(p.Dir, name), p.Go); err != nil {
		return err
	}
	if stale {
		if err = os.Remove(otherPath); err != nil {
			return err
		}
	}
	if ts != nil {
		if err = ts.apply(args.Force); err != nil {
			return err
		}
	}
	if js != nil {
		if err = js.apply(args.Force); err != nil {
			return err
		}
	}
	return nil
}

func (p emissionPlan) applySchemas(noChanges, force bool) (changed, orphans []string, err error) {
	dir := filepath.Join(p.Dir, p.SchemaDir)
	if len(p.Schemas) == 0 {
		if _, e := os.Stat(dir); errors.Is(e, os.ErrNotExist) {
			return nil, nil, nil
		}
	} else if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, nil, fmt.Errorf("could not create subdir %s: %w", dir, err)
	}
	expected := map[string]bool{}
	for _, s := range p.Schemas {
		expected[s.Name] = true
		expected[s.Name+".sum"] = true
	}
	orphans, err = pruneOrphanedSchemaArtifacts(dir, expected, true)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range p.Schemas {
		h := fnv.New64a()
		_, _ = h.Write(s.Content)
		sum := hex.EncodeToString(h.Sum(nil))
		path := filepath.Join(dir, s.Name)
		old, e := os.ReadFile(path + ".sum")
		different := e != nil || string(old) != sum
		if different || force {
			changed = append(changed, s.Type)
		}
		if different && noChanges {
			continue
		}
		if err = writeOutputFile(path, s.Content); err != nil {
			return nil, nil, err
		}
		if err = writeOutputFile(path+".sum", []byte(sum)); err != nil {
			return nil, nil, err
		}
	}
	if !noChanges && len(orphans) > 0 {
		orphans, err = pruneOrphanedSchemaArtifacts(dir, expected, false)
	}
	return
}
