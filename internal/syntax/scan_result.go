package syntax

import (
	"fmt"
	"go/constant"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"
)

type PackageScanner interface {
	Scan(*packages.Package) (ScanResult, error)
}

// TypeDecl refers to the
type TypeDecl struct {
	Node  *dst.DeclStmt
	Pkg   *packages.Package
	File  *dst.File
	Pos   token.Pos
	Decls []TypeInfo
}

type InterfaceTypeInfo struct {
	typeInfo
	Implementations []TypeID
}

// AliasTypeInfo
// refers to type alias declarations.
type AliasTypeInfo struct {
	typeInfo
}

// DefinitionTypeInfo
// A Definition type Refers to any type that's declared as an instance of another type,
// including type-instantiated generics.
// e.g.:
// ```
// type MyInt int
// type MyString string
// type MyFooType somepackage.SomeGenericType[MyInt, MyType]
// ```
type DefinitionTypeInfo struct {
	typeInfo
}

var _ TypeInfo = InterfaceTypeInfo{}

type typeInfo struct {
	Identity *dst.Ident
	TypeInfo dst.Expr
	File     *dst.File
	Pkg      *packages.Package
	Decl     *TypeDecl
}

type TypeInfo interface {
	GetTypeName() string
	GetPkgPath() string
	GetTypeInfo() dst.Expr
	GetFile() *dst.File
	GetPkg() *packages.Package
	GetDecl() *TypeDecl
}

func (t typeInfo) GetDecl() *TypeDecl {
	return t.Decl
}

// Getters for typeInfo fields.
func (t typeInfo) GetTypeName() string {
	return t.Identity.Name
}

func (t typeInfo) GetPkgPath() string {
	return t.Pkg.PkgPath
}

func (t typeInfo) GetTypeInfo() dst.Expr {
	return t.TypeInfo
}

func (t typeInfo) GetFile() *dst.File {
	return t.File
}

func (t typeInfo) GetPkg() *packages.Package {
	return t.Pkg
}

type (
	SchemaMethodOptionKind string

	SchemaMethodOptionInfo struct {
		Kind             SchemaMethodOptionKind
		FieldName        string
		ProviderName     string
		ProviderIsMethod bool
	}

	TypeDecls struct {
		Pkg  *decorator.Package
		File *dst.File

		Decl  *dst.GenDecl
		Specs []*dst.TypeSpec
	}

	VarDecls struct {
		Pkg   *decorator.Package
		File  *dst.File
		Decl  *dst.GenDecl
		Specs []*dst.ValueSpec
	}

	SchemaMethod struct {
		Receiver         TypeID
		SchemaMethodName string
		MarkerCall       MarkerFunctionCall
		Options          []SchemaMethodOptionInfo
	}
	SchemaFunction SchemaMethod

	// IfaceImplementations is a sealed interface and its inferred wire
	// membership: every named struct type in the interface's own package that
	// declares the sealing method directly (see inferSealedUnion). Impls
	// carry Pointer indirection for pointer-receiver variants. Discriminator
	// is the union's declared discriminator property, empty for the default.
	IfaceImplementations struct {
		TypeSpec      TypeSpec
		Impls         []TypeID
		Discriminator string
	}

	EnumSet struct {
		TypeSpec TypeSpec
		Values   []EnumValue
	}

	// EnumValue is one package-level constant whose exact Go type is the
	// registered enum type. Value is the evaluated constant, not its source
	// spelling, and therefore handles iota, expressions and conversions.
	EnumValue struct {
		Name        string
		Value       constant.Value
		Description string
		Source      token.Position
	}
)

func (s SchemaMethod) IsPointer() bool {
	return s.Receiver.Indirection == Pointer
}

func (s SchemaMethod) markerType() MarkerKind {
	return MarkerKindSchema
}

func (c EnumSet) markerType() MarkerKind {
	return MarkerKindEnum
}

func (i IfaceImplementations) markerType() MarkerKind {
	return MarkerKindInterface
}

// Marker is the common interface for all returned marker structs.
type Marker interface {
	markerType() MarkerKind
}

var (
	_ Marker = IfaceImplementations{}
	_ Marker = SchemaMethod{}
	_ Marker = EnumSet{}
)

// MarkerKind enumerates the four categories of markers we have.
type MarkerKind string

const (
	MarkerKindEnum      MarkerKind = "Enum"
	MarkerKindInterface MarkerKind = "InterfaceImpl"
	MarkerKindSchema    MarkerKind = "Schema"
)

type VarDeclSet []VarConstDecl

func (vd VarDeclSet) MarkerFuncs() []MarkerFunctionCall {
	var result []MarkerFunctionCall
	for _, decl := range vd {
		for _, spec := range decl.Specs() {
			result = append(
				result,
				ParseValueExprForMarkerFunctionCall(spec)...,
			)
		}
	}
	return result
}

type decls struct {
	constDecls []VarConstDecl
	typeDecls  []TypeDecls
	varDecls   VarDeclSet
	funcDecls  []FuncDecl
}

type ScanResult struct {
	Pkg         *decorator.Package
	Constants   map[string]*EnumSet
	MarkerCalls []MarkerFunctionCall
	Interfaces  map[string]IfaceImplementations
	// InterfaceDiagnostics records, per named interface type that is not a
	// usable sealed union, why it was rejected. The builder reports the
	// diagnostic when a generated schema reaches the interface.
	InterfaceDiagnostics map[string]error
	localTypeNames       map[string]bool
	SchemaMethods        []SchemaMethod
	SchemaFuncs          []SchemaFunction
	LocalNamedTypes      map[string]TypeSpec
	remoteTypes          typesMap
	deps                 map[string]ScanResult
	// temp variable used during resolution only.
	resolveQueue            []TypeSpec
	alreadyTraversedLocally map[string]bool
}

func (s ScanResult) GetPackage(pkgPath string) (ScanResult, bool) {
	if pkgPath == s.Pkg.PkgPath {
		return s, true
	}
	res, ok := s.deps[pkgPath]
	return res, ok
}

type seenPackages []string

func (s seenPackages) seen(pkg *decorator.Package) bool {
	return slices.Contains(s, pkg.ID)
}

func (s seenPackages) see(pkg *decorator.Package) seenPackages {
	return append(seenPackages{pkg.ID}, s...)
}

func (s seenPackages) add(pkg *decorator.Package) (seenPackages, bool) {
	if s.seen(pkg) {
		return s, false
	}
	return s.see(pkg), true
}

// LoadPackage is the main entry point that creates a ScanResult for the given package.
// Note: we pass a non-nil map to loadPackageInternal(...) so we can safely store references
// to local types without panicking.
func LoadPackage(pkg *decorator.Package) (res ScanResult, err error) {
	res = newScanResult(pkg, map[string]ScanResult{})
	// Pass an empty map so we never do `typesToMap[foo] = true` on a nil map.
	err = res.loadPackageInternal(seenPackages{}, make(map[string]bool))
	return
}

func loadPackageForTest(pkg *decorator.Package, typesToInclude ...string) (ScanResult, error) {
	var types = make(map[string]bool)
	for _, typeName := range typesToInclude {
		types[typeName] = true
	}
	scanResult := newScanResult(pkg, map[string]ScanResult{})
	err := scanResult.loadPackageInternal(seenPackages{}, types)
	return scanResult, err
}

func newScanResult(pkg *decorator.Package, deps map[string]ScanResult) ScanResult {
	return ScanResult{
		Pkg:                     pkg,
		Constants:               make(map[string]*EnumSet),
		MarkerCalls:             make([]MarkerFunctionCall, 0),
		Interfaces:              make(map[string]IfaceImplementations),
		InterfaceDiagnostics:    make(map[string]error),
		SchemaMethods:           make([]SchemaMethod, 0),
		SchemaFuncs:             make([]SchemaFunction, 0),
		LocalNamedTypes:         make(map[string]TypeSpec),
		remoteTypes:             typesMap{},
		localTypeNames:          make(map[string]bool),
		deps:                    deps,
		alreadyTraversedLocally: make(map[string]bool),
	}
}

type typesMap map[string]map[string]bool

func (t typesMap) addType(pkgPath, typeName string) {
	if t[pkgPath] == nil {
		t[pkgPath] = map[string]bool{typeName: true}
	} else {
		t[pkgPath][typeName] = true
	}
}

func (r *ScanResult) resolveTypeLocal(name string) (Expr, error) {
	if iface, ok := r.Interfaces[name]; ok {
		return iface.TypeSpec.Type(), nil
	}
	t, ok := r.LocalNamedTypes[name]
	if !ok {
		knownTypes := make([]string, 0, len(r.LocalNamedTypes))
		for typeName := range r.LocalNamedTypes {
			knownTypes = append(knownTypes, typeName)
		}
		slices.Sort(knownTypes)
		return nil, fmt.Errorf(
			"resolveTypeLocal: type %s not found; known local types: %s",
			name,
			strings.Join(knownTypes, ", "),
		)
	}
	if ident, ok := t.Type().Expr().(*dst.Ident); ok {
		return r.resolveType(r.Pkg.PkgPath, IdentExpr{STExpr: NewExpr(ident, t.Pkg(), t.File())})
	}
	return t.Type(), nil
}

func setAllIdentPaths(node dst.Node, path string) {
	dst.Inspect(node, func(n dst.Node) bool {
		switch typ := n.(type) {
		case *dst.StructType:
			for _, field := range typ.Fields.List {
				setAllIdentPaths(field.Type, path)
			}
			return false
		case *dst.Ident:
			if !BasicTypes[typ.Name] {
				typ.Path = path
			}
			return false
		}
		return true
	})
}

func (r *ScanResult) resolveTypeRemote(path, name string) (Expr, error) {
	scanResult, ok := r.GetPackage(path)
	if !ok {
		return nil, fmt.Errorf("package %s not found", path)
	}
	// The thing is, we have to translate the remote expression into one that
	// is valid locally.  That means that the resulting Expr should have the local
	// package and any unqualified idents should have their Path set.
	expr, err := scanResult.resolveTypeLocal(name)
	if err != nil {
		return nil, err
	}
	itsNode := dst.Clone(expr.Node())
	setAllIdentPaths(itsNode, path)
	return expr.NewExpr(itsNode.(dst.Expr)), nil
}

func (r *ScanResult) resolveType(pkgPath string, ident IdentExpr) (Expr, error) {
	e := ident.Concrete
	if e.Path == "" {
		s, ok := r.GetPackage(pkgPath)
		if !ok {
			return nil, fmt.Errorf("package %s not found", pkgPath)
		}
		return s.resolveTypeLocal(e.Name)
	} else {
		result, err := r.resolveTypeRemote(e.Path, e.Name)
		if err != nil {
			return nil, err
		}
		return ident.NewExpr(result.Expr()), nil
	}
}

func (r *ScanResult) loadPackageInternal(seen seenPackages, typesToMap map[string]bool) error {
	if typesToMap == nil {
		// Safety check in case it's ever passed nil from some other call site
		typesToMap = make(map[string]bool)
	}

	var (
		_, ok  = seen.add(r.Pkg)
		_decls = loadPkgDecls(r.Pkg)
		// sealedUnionDeclarations collects SealedUnion[I](name) markers by
		// interface name until the type declarations have been classified.
		sealedUnionDeclarations = map[string]sealedUnionDeclaration{}
	)
	if !ok {
		return fmt.Errorf("circular package dependency detected. %v", seen)
	}

	r.MarkerCalls = _decls.varDecls.MarkerFuncs()
	for _, decl := range r.MarkerCalls {
		switch decl.CallExpr.MustIdentifyFunc().TypeName {
		case MarkerFuncNewJSONSchemaMethod:
			method, err := decl.ParseSchemaMethod()
			if err != nil {
				return err
			}
			r.localTypeNames[method.Receiver.TypeName] = true
			typesToMap[method.Receiver.TypeName] = true
			r.SchemaMethods = append(r.SchemaMethods, method)
		case MarkerFuncNewJSONSchemaBuilder:
			fn, err := decl.ParseSchemaBuilder()
			if err != nil {
				return err
			}
			r.localTypeNames[fn.Receiver.TypeName] = true
			typesToMap[fn.Receiver.TypeName] = true
			r.SchemaFuncs = append(r.SchemaFuncs, fn)
		case MarkerFuncNewJSONSchemaFunc:
			fn, err := decl.ParseSchemaFunc()
			if err != nil {
				return err
			}
			r.localTypeNames[fn.Receiver.TypeName] = true
			typesToMap[fn.Receiver.TypeName] = true
			r.SchemaFuncs = append(r.SchemaFuncs, fn)

		case MarkerFuncDeclare:
			method, isMethodRoot, err := decl.ParseFluentDeclaration(_decls.funcDecls)
			if err != nil {
				return err
			}
			r.localTypeNames[method.Receiver.TypeName] = true
			typesToMap[method.Receiver.TypeName] = true
			if isMethodRoot {
				r.SchemaMethods = append(r.SchemaMethods, method)
			} else {
				r.SchemaFuncs = append(r.SchemaFuncs, SchemaFunction(method))
			}

		case MarkerFuncSealedUnion:
			if err := r.parseSealedUnionDeclaration(decl, sealedUnionDeclarations); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported marker function: %s", decl.CallExpr.MustIdentifyFunc())
		}
	}

	for _, _typeDecl := range _decls.typeDecls {
		for _, spec := range _typeDecl.Specs {
			typeSpec := NewTypeSpec(_typeDecl.Decl, spec, _typeDecl.Pkg, _typeDecl.File)
			if _, isInterface := spec.Type.(*dst.InterfaceType); isInterface {
				// Sealed unions are inferred from the interface's own
				// unexported method(s). Anything that disqualifies the
				// interface is recorded, not raised: the diagnostic surfaces
				// only when a generated schema actually reaches the type.
				variants, err := inferSealedUnion(r.Pkg.Types, spec.Name.Name, typeSpec.Position())
				if err != nil {
					r.InterfaceDiagnostics[spec.Name.Name] = err
					r.LocalNamedTypes[spec.Name.Name] = typeSpec
					continue
				}
				r.Interfaces[spec.Name.Name] = IfaceImplementations{TypeSpec: typeSpec, Impls: variants}
				continue
			}
			marked, err := hasEnumMarker(r.Pkg.Types, spec.Name.Name, typeSpec.Position())
			if err != nil {
				return err
			}
			if marked {
				resolved, err := ResolveEnum(typeSpec)
				if err != nil {
					return err
				}
				if len(resolved.Values) == 0 {
					return fmt.Errorf("enum type %s at %s declares enum() but has no typed constants", spec.Name.Name, typeSpec.Position())
				}
				r.Constants[spec.Name.Name] = resolved
				continue
			}
			r.LocalNamedTypes[spec.Name.Name] = typeSpec
		}
	}
	if err := r.applySealedUnionDeclarations(sealedUnionDeclarations); err != nil {
		return err
	}
	for _, iface := range r.Interfaces {
		for _, impl := range iface.Impls {
			r.localTypeNames[impl.TypeName] = true
			typesToMap[impl.TypeName] = true
		}
	}
	for typeName := range typesToMap {
		if err := r.requestType(typeName); err != nil {
			return err
		}
	}

	if err := r.resolveTypes(); err != nil {
		return err
	}
	return nil
}

func (r *ScanResult) resolveTypeExpr(_expr Expr, seen SeenTypes) error {
	switch expr := _expr.Expr().(type) {
	case *dst.IndexExpr, *dst.IndexListExpr:
		if kind, _, ok := wrapperExpr(_expr); ok {
			return fmt.Errorf("%s is supported only as the complete type of a direct named struct field at %s", kind, _expr.Position())
		}
		return fmt.Errorf("unsupported generic type %s at %s", _expr.Details(), _expr.Position())
	case *dst.ParenExpr:
		return r.resolveTypeExpr(_expr.NewExpr(expr.X), seen)
	case *dst.StarExpr:
		return r.resolveTypeExpr(_expr.NewExpr(expr.X), seen)
	case *dst.ArrayType:
		return r.resolveTypeExpr(_expr.NewExpr(expr.Elt), seen)
	case *dst.MapType:
		// This is a discovery boundary, not validation or rendering support.
		// The renderer independently rejects maps. Discovery must not recurse
		// into a shape that cannot be rendered, but it also must not prevent
		// unrelated syntax operations such as flattening from loading a package.
		return nil
	case *dst.InterfaceType:
		// This is a discovery boundary, not validation or rendering support.
		// Named interfaces registered through v1 schema options remain in
		// LocalNamedTypes and are resolved by the builder; unmatched interfaces
		// are independently rejected by the renderer.
		return nil
	case *dst.StructType:
		for _, field := range expr.Fields.List {
			if skipField(field) {
				continue
			}
			fieldExpr := _expr.NewExpr(field.Type)
			if kind, args, ok := wrapperExpr(fieldExpr); ok {
				if len(field.Names) == 0 {
					return fmt.Errorf("embedded %s is unsupported at %s", kind, fieldExpr.Position())
				}
				if len(args) != 1 {
					return fmt.Errorf("%s requires exactly one type argument at %s", kind, fieldExpr.Position())
				}
				if err := r.resolveTypeExpr(fieldExpr.NewExpr(args[0]), seen); err != nil {
					return fmt.Errorf("%s inner type at %s: %w", kind, fieldExpr.Position(), err)
				}
				continue
			}
			if err := r.resolveTypeExpr(fieldExpr, seen); err != nil {
				return fmt.Errorf("struct field at %s: %w", _expr.NewExpr(field.Type).Position(), err)
			}
		}
	case *dst.Ident:
		if expr.Path == "" || expr.Path == r.Pkg.PkgPath {
			// It's either a basic type or a locally-defined named type
			if BasicTypes[expr.Name] {
				return nil // basic type
			}
			if named, ok := r.LocalNamedTypes[expr.Name]; ok {
				var added bool
				seen, added = seen.Add(named.ID())
				if !added {
					return fmt.Errorf("cyclic dependency found at %s", named.Position())
				}
				if r.alreadyTraversedLocally[expr.Name] {
					return nil
				}
				if err := r.resolveTypeExpr(named.Type(), seen); err != nil {
					return err
				}
				r.alreadyTraversedLocally[expr.Name] = true
				return nil
			}
			if r.Constants[expr.Name] != nil {
				return nil
			}
			if _, ok := r.Interfaces[expr.Name]; ok {
				return nil
			}
			return fmt.Errorf("undeclared local %s type found: %s at %s", expr.Name, _expr.Details(), _expr.Position())
		} else {
			if !IsTimeType(expr.Path, expr.Name) {
				r.remoteTypes.addType(expr.Path, expr.Name)
			}
		}
	case *dst.SelectorExpr:
		// Instead of panicking, at least store the remote reference
		if xIdent, ok := expr.X.(*dst.Ident); ok {
			pkgPath := xIdent.Path
			if pkgPath == "" {
				// Fallback if there's no path, treat the 'X' as the package name.
				pkgPath = xIdent.Name
			}
			if IsTimeType(pkgPath, expr.Sel.Name) {
				return nil
			}
			r.remoteTypes.addType(pkgPath, expr.Sel.Name)
		} else {
			return fmt.Errorf("unhandled selector expression: %s", _expr.Details())
		}
	case *dst.BasicLit:
		return nil
	default:
		return fmt.Errorf("unhandled expression %s", _expr.Details())
	}
	return nil
}

// skipField determines whether the type traversal should descend into a given
// field.  There are three cases for skipping:
//
// 1. The field is annotated as jsonschema:"-"
// 2. The field not exported (lower case name)
// 3. The field is annotated as a "ref"
//
// Ref example:
// ```go
//
//	type User struct {
//	    ID       UserID   `json:"id"`
//	    Username Username `json:"username" jsonschema:"ref=definitions/User"`
//	}
//
// ```
func skipField(field *dst.Field) bool {
	if len(field.Names) == 0 { // don't skip embedded
		return false
	}
	if !hasExportedFieldName(field) {
		return true
	}
	if fieldJSONIgnored(field) {
		return true
	}
	if tagEntry := fieldStructTag(field, "jsonschema"); tagEntry != nil {
		if _, ok := tagEntry.GetOptValue("ref"); ok {
			return true
		}
	}
	return false
}

func (r *ScanResult) resolveTypes() error {
	var (
		ts  TypeSpec
		err error
	)
	for len(r.resolveQueue) > 0 {
		ts = r.resolveQueue[0]
		r.resolveQueue = r.resolveQueue[1:]
		if r.alreadyTraversedLocally[ts.Concrete.Name.Name] {
			continue
		}
		// Pass a non-nil "seen" so we can detect cycles properly.
		if err = r.resolveTypeExpr(NewExpr(ts.Concrete.Type, ts.pkg, ts.file), nil); err != nil {
			return fmt.Errorf("resolving Concrete at %s: %w", ts.Position(), err)
		}
		r.alreadyTraversedLocally[ts.Name()] = true
	}
	for pkgPath, typeNames := range r.remoteTypes {
		if remote, ok := r.deps[pkgPath]; ok {
			for typeName := range typeNames {
				if err = remote.requestType(typeName); err != nil {
					return fmt.Errorf("resolving type at %s: %w", pkgPath, err)
				}
			}
			if err = remote.resolveTypes(); err != nil {
				return fmt.Errorf("resolving type at %s: %w", pkgPath, err)
			}
		} else if pkgs, err := r.loadDependency(pkgPath); err != nil {
			return err
		} else {
			remote = newScanResult(pkgs[0], r.deps)
			if err = remote.loadPackageInternal(seenPackages{}, typeNames); err != nil {
				return fmt.Errorf("resolving type at %s: %w", pkgPath, err)
			}
			r.deps[pkgPath] = remote
		}
	}
	return nil
}

// loadDependency resolves an imported package by import path from the loading
// package's own directory. Resolving it from the process working directory
// instead would look it up in the wrong module whenever the loaded package is
// not the one the process was started in.
func (r *ScanResult) loadDependency(pkgPath string) ([]*decorator.Package, error) {
	files := r.Pkg.GoFiles
	if len(files) == 0 {
		files = r.Pkg.CompiledGoFiles
	}
	if len(files) == 0 {
		return Load(pkgPath)
	}
	return LoadFrom(filepath.Dir(files[0]), pkgPath)
}

// EnsureRemoteType loads pkgPath as a dependency of r, if it is not loaded
// already, and resolves typeName inside it. Dependency packages otherwise
// enter deps only through marker-seeded traversal, so a caller that reaches a
// named type without a marker (a caller-supplied lowering root) must ask for
// the package on demand. Loading is per package, not per module.
func (r *ScanResult) EnsureRemoteType(pkgPath, typeName string) error {
	if pkgPath == "" || pkgPath == r.Pkg.PkgPath {
		return nil
	}
	r.remoteTypes.addType(pkgPath, typeName)
	return r.resolveTypes()
}

// EnsureType resolves a caller-selected named type and all of its reachable
// source dependencies. Unlike marker-seeded loading, it also handles a root
// selected programmatically from the package currently being scanned.
func (r *ScanResult) EnsureType(pkgPath, typeName string) error {
	if pkgPath == "" || pkgPath == r.Pkg.PkgPath {
		if err := r.requestType(typeName); err != nil {
			return err
		}
		return r.resolveTypes()
	}
	return r.EnsureRemoteType(pkgPath, typeName)
}

func (r *ScanResult) requestType(typeName string) error {
	if named, ok := r.LocalNamedTypes[typeName]; ok {
		alreadyQueued := slices.ContainsFunc(r.resolveQueue, func(queued TypeSpec) bool {
			return queued.Name() == typeName
		})
		if !r.alreadyTraversedLocally[typeName] && !alreadyQueued {
			r.resolveQueue = append(r.resolveQueue, named)
		}
		return nil
	}
	if r.Constants[typeName] != nil {
		return nil
	}
	if iface, ok := r.Interfaces[typeName]; ok {
		for _, impl := range iface.Impls {
			if err := r.requestType(impl.TypeName); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("undeclared local type found: %s", typeName)
}

func loadPkgDecls(pkg *decorator.Package) *decls {
	var (
		_decls decls
	)
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch _decl := decl.(type) {
			case *dst.FuncDecl:
				_decls.funcDecls = append(_decls.funcDecls, NewFuncDecl(_decl, pkg, file))
			case *dst.GenDecl:
				switch _decl.Tok {
				case token.TYPE:
					var specs []*dst.TypeSpec
					for _, spec := range _decl.Specs {
						specs = append(specs, spec.(*dst.TypeSpec))
					}
					_decls.typeDecls = append(_decls.typeDecls, TypeDecls{
						Pkg:   pkg,
						File:  file,
						Decl:  _decl,
						Specs: specs,
					})
				case token.CONST:
					_decls.constDecls = append(_decls.constDecls, NewVarConstDecl(_decl, pkg, file))
				case token.VAR:
					_decls.varDecls = append(_decls.varDecls, NewVarConstDecl(_decl, pkg, file))
				default:
				}
			}
		}
	}
	return &_decls
}

var BasicTypes = map[string]bool{
	"int":        true,
	"int8":       true,
	"int16":      true,
	"int32":      true,
	"int64":      true,
	"uint":       true,
	"uint8":      true,
	"uint16":     true,
	"uint32":     true,
	"uint64":     true,
	"uintptr":    true,
	"string":     true,
	"bool":       true,
	"float32":    true,
	"float64":    true,
	"complex64":  true,
	"complex128": true,
	"byte":       true, // alias for uint8
	"rune":       true, // alias for int32
	"error":      true,
}
