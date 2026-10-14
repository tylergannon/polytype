package builder

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tylergannon/polytype/goload"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typescript"
)

// RunCachedTypeScript serves the existing codegen.Generate TypeScript-only
// request. It returns false for opaque configuration or unavailable cache
// acquisition, leaving the established general programmatic path in charge.
func RunCachedTypeScript(target string, config ProgrammaticConfig, dir string, barrel, force bool, session *goload.Session) (bool, error) {
	identity, ok := typeScriptIdentity(config)
	if !ok {
		return false, nil
	}
	if dir == "" {
		dir = "."
	}
	args := BuilderArgs{TargetDir: target, TypeScriptDir: dir, TypeScriptBarrel: barrel, Force: force}
	inputs, err := acquireCacheFor(args, identity)
	if err != nil {
		cacheTrace("bypass: " + err.Error())
		return false, nil
	}
	return runCachedTypeScriptInputs(inputs, args, config, barrel, session)
}

func runCachedTypeScriptInputs(inputs *cacheInputs, args BuilderArgs, config ProgrammaticConfig, barrel bool, session *goload.Session) (bool, error) {
	if plan, hit := inputs.read(); hit {
		cacheTrace("hit")
		return true, applyConfiguredTypeScript(plan, args)
	}
	cacheTrace("miss")
	inputs.session = session
	pkgs, err := inputs.load(".")
	if err != nil {
		return true, err
	}
	if len(pkgs) == 0 {
		return true, fmt.Errorf("no packages found for %s", args.TargetDir)
	}
	scan, err := syntax.LoadConfiguredPackageWithSource(pkgs[0], inputs.load, inputs.source)
	if err != nil {
		return true, err
	}
	b, err := newProgrammaticFromScan(scan, config, false, false)
	if err != nil {
		return true, err
	}
	if err = b.ApplyTransforms(); err != nil {
		return true, err
	}
	definitions, _, err := b.ConfiguredTypeDefinitions()
	if err != nil {
		return true, err
	}
	result, err := typescript.Generate(definitions, typescript.Options{Barrel: barrel})
	if err != nil {
		return true, err
	}
	// The common entry envelope stores a publication-independent emission. This
	// request owns TypeScript only: it must never inspect/write/prune Go/schema.
	plan := emissionPlan{Dir: inputs.dir, SchemaDir: defaultSubdir, TypeScript: result.Files}
	inputs.save(plan)
	return true, applyConfiguredTypeScript(plan, args)
}

// Programmatic generation historically replaces its selected files and does
// not prune siblings. Replaying its cache must preserve those same semantics.
func applyConfiguredTypeScript(plan emissionPlan, args BuilderArgs) error {
	if err := os.MkdirAll(args.TypeScriptDir, 0755); err != nil {
		return err
	}
	for _, file := range plan.TypeScript {
		if file.Name != typeScriptTypesFile && file.Name != typeScriptBarrelFile {
			return fmt.Errorf("unexpected TypeScript output filename %q", file.Name)
		}
		if err := writeOutputFile(filepath.Join(args.TypeScriptDir, file.Name), file.Content); err != nil {
			return err
		}
	}
	return nil
}

func typeScriptIdentity(config ProgrammaticConfig) (any, bool) {
	type unionIdentity struct{ PackagePath, TypeName, Discriminator string }
	identity := struct {
		Mode         string
		Declarations []ConfiguredDeclaration
		Unions       []unionIdentity
	}{Mode: "configured-typescript", Declarations: config.Declarations}
	for _, union := range config.SealedUnions {
		if union.Inflect != nil {
			return nil, false
		}
		identity.Unions = append(identity.Unions, unionIdentity{union.PackagePath, union.TypeName, union.Discriminator})
	}
	return identity, true
}
