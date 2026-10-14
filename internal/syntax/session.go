package syntax

import (
	"path/filepath"

	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/goload"
	"golang.org/x/tools/go/packages"
)

// LoadWithSession keeps the standard decorated syntax boundary while sharing
// its underlying immutable Go analysis. Decoration itself is consumer-owned.
func LoadWithSession(cfg *packages.Config, session *goload.Session, patterns ...string) ([]*decorator.Package, error) {
	if session == nil {
		return LoadWithLoader(cfg, nil, patterns...)
	}
	return LoadWithLoader(cfg, session.Load, patterns...)
}

// LoadWithLoader decorates results from a caller-owned package loader.
func LoadWithLoader(cfg *packages.Config, loader goload.LoadFunc, patterns ...string) ([]*decorator.Package, error) {
	if loader == nil {
		return decorator.Load(cfg, patterns...)
	}
	pkgs, err := loader(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	converted := map[*packages.Package]*decorator.Package{}
	var convert func(*packages.Package) (*decorator.Package, error)
	convert = func(p *packages.Package) (*decorator.Package, error) {
		if old := converted[p]; old != nil {
			return old, nil
		}
		out := &decorator.Package{Package: p, Imports: map[string]*decorator.Package{}}
		converted[p] = out
		if len(p.Syntax) == 0 {
			return out, nil
		}
		out.Decorator = decorator.NewDecoratorFromPackage(p)
		files := map[string]bool{}
		for _, name := range p.GoFiles {
			files[name] = true
		}
		for _, f := range p.Syntax {
			name := p.Fset.File(f.Pos()).Name()
			if !files[name] {
				continue
			}
			d, e := out.Decorator.DecorateFile(f)
			if e != nil {
				return nil, e
			}
			out.Syntax = append(out.Syntax, d)
			if out.Dir == "" {
				out.Dir = filepath.Dir(name)
			}
		}
		for path, dep := range p.Imports {
			d, e := convert(dep)
			if e != nil {
				return nil, e
			}
			out.Imports[path] = d
		}
		return out, nil
	}
	out := make([]*decorator.Package, 0, len(pkgs))
	for _, p := range pkgs {
		d, e := convert(p)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, nil
}
