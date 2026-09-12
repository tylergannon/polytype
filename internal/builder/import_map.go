package builder

import (
	"fmt"

	"github.com/dave/dst/decorator"
)

// ImportMap helps with code generation by storing a list of packages
// along with aliases.  The alias can be looked up using the package object,
// which is stored alongside our AST nodes in QuestImpl and TaskImpl objects.
type ImportMap struct {
	localPackage *decorator.Package
	usedNames    map[string]bool
	types        []struct {
		alias string
		pkg   *decorator.Package
	}
}

func (m *ImportMap) LocalPkgName() string {
	return m.localPackage.Name
}

func NewImportMap(localPackage *decorator.Package) *ImportMap {
	usedNames := map[string]bool{}
	for _, name := range []string{"bytes", "embed", "errors", "fmt", "json", "jsonschema", "template"} {
		usedNames[name] = true
	}
	return &ImportMap{localPackage: localPackage, usedNames: usedNames}
}

// AddPackage inserts the package unless it is already present. It allocates an
// alias that cannot collide with template imports or any prior effective alias.
func (m *ImportMap) AddPackage(pkg *decorator.Package) {
	if m.localPackage.ID == pkg.ID {
		return
	}
	newObj := struct {
		alias string
		pkg   *decorator.Package
	}{pkg: pkg}

	for _, t := range m.types {
		if t.pkg.ID == pkg.ID {
			return
		}
	}
	name := pkg.Name
	if m.usedNames[name] {
		for suffix := 1; ; suffix++ {
			candidate := fmt.Sprintf("%s%d", pkg.Name, suffix)
			if !m.usedNames[candidate] {
				name = candidate
				break
			}
		}
	}
	if name != pkg.Name {
		newObj.alias = name
	}
	m.usedNames[name] = true
	m.types = append(m.types, newObj)
}

// PrefixExpr is a function that should be added to the template funcs when
// building a template object.  It correctly prints a type name or call
// expression using the right package name prefix/alias (or none if the
// expression refers to an identifier defined in the local package).
func (m *ImportMap) PrefixExpr(expr string, pkg *decorator.Package) string {
	if pkg.ID == m.localPackage.ID {
		return expr
	}
	return fmt.Sprintf("%s.%s", m.Alias(pkg), expr)
}

func (m *ImportMap) ImportStatements() []string {
	var result []string
	for _, t := range m.types {
		// Note that we'll use `goimports` on this file later so imports will be
		// cleaned up and ordered.  Don't worry about the extra whitespace here.
		name := t.pkg.Name
		if t.alias != "" {
			name = t.alias
		}
		result = append(result, fmt.Sprintf("%s \"%s\"", name, t.pkg.PkgPath))
	}

	return result
}

func (m *ImportMap) Alias(pkg *decorator.Package) string {
	for _, t := range m.types {
		if t.pkg.ID == pkg.ID {
			if t.alias == "" {
				return pkg.Name
			}
			return t.alias
		}
	}
	panic("called Alias with package that's not registered")
}
