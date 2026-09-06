// Package grammar is the exported entry point for lowering Go types into the
// static type-definition grammar of
// github.com/tylergannon/polytype/typegrammar.
//
// Load reads a Go package the way the polytype CLI does, and Lower turns
// caller-chosen root types into a validated definition graph plus one grammar
// node per root. Callers use it to write their own code-generation backend
// without going through the CLI or asking for a schema file.
//
// Roots are matched structurally and by name, so a *types.Type obtained from
// the caller's own packages.Load works. A named root does not need a Declare
// marker; whatever the CLI would refuse in a struct field (maps, channels,
// functions, unconfigured interfaces, presence wrappers) is refused here with
// the same wording.
package grammar

import (
	"fmt"
	"go/token"
	"go/types"

	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// Package is a loaded Go package that roots can be lowered against.
type Package struct {
	builder builder.SchemaBuilder
}

// Root is one shape a caller wants lowered. Type may be anonymous; Position is
// reported in diagnostics because an anonymous type has no source location of
// its own.
type Root struct {
	Type     types.Type
	Position token.Position
}

// Load loads the Go package at dir with the jsonschema build tag, the same way
// the CLI does, together with the packages it references.
func Load(dir string) (*Package, error) {
	pkgs, err := syntax.Load(dir)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found in %s", dir)
	}
	if errs := pkgs[0].Errors; len(errs) > 0 {
		return nil, fmt.Errorf("package %s has errors: %s", pkgs[0].PkgPath, errs[0])
	}
	b, err := builder.New(pkgs[0])
	if err != nil {
		return nil, err
	}
	return &Package{builder: b}, nil
}

// Types is the package's type-checked scope, for looking up the roots to lower.
func (p *Package) Types() *types.Package {
	return p.builder.Scan.Pkg.Types
}

// Lower returns the validated definition graph reachable from the roots, and
// one grammar node per root in order. Named roots need no Declare marker.
func (p *Package) Lower(roots []Root) (typegrammar.Definitions, []typegrammar.Type, error) {
	if p == nil {
		return nil, nil, fmt.Errorf("lower: nil Package")
	}
	converted := make([]builder.RootType, len(roots))
	for i, root := range roots {
		if root.Type == nil {
			return nil, nil, fmt.Errorf("lower root %d: nil type", i)
		}
		converted[i] = builder.RootType{Type: root.Type, Position: root.Position}
	}
	return p.builder.LowerRoots(converted)
}
