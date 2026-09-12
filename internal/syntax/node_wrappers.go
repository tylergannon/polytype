package syntax

import (
	"fmt"
	"go/token"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/tylergannon/structtag"
)

type (
	// WrapperKind classifies the public presence wrappers supported on direct
	// named struct fields.
	WrapperKind uint8

	STNode[T dst.Node] struct {
		Concrete T
		file     *dst.File
		pkg      *decorator.Package
	}

	Node interface {
		Node() dst.Node
		File() *dst.File
		Pkg() *decorator.Package
		Pos() token.Pos
		Position() token.Position
		Imports() ImportMap
		Details() string
	}

	STExpr[T dst.Expr] struct {
		STNode[T]
	}

	Expr interface {
		Node
		NewExpr(expr dst.Expr) Expr
		Expr() dst.Expr
	}

	// TypeExpr is an Expr that is specifically an excerpt from
	// a type declaration.
	// The main idea is mostly to anchor the expr to a specific TypeID.
	// Not 100% sure that's necessary, strictly speaking, but the goal here
	// is more to ensure that there's sufficient information to build
	// the schemas and their associated artifacts.
	TypeExpr struct {
		*TypeSpec
		Excerpt dst.Expr
	}

	CallExpr struct {
		STExpr[*dst.CallExpr]
	}

	ValueSpec struct {
		GenDecl STNode[*dst.GenDecl]
		STNode[*dst.ValueSpec]
	}

	TypeSpec struct {
		GenDecl STNode[*dst.GenDecl]
		STNode[*dst.TypeSpec]
	}

	StructType struct {
		TypeSpec
		Expr *dst.StructType
	}

	StructField struct {
		TypeExpr
		Field *dst.Field
	}

	VarConstDecl struct {
		STNode[*dst.GenDecl]
	}

	FuncDecl struct {
		STNode[*dst.FuncDecl]
	}

	IdentExpr struct {
		STExpr[*dst.Ident]
		InterfaceType bool
	}
)

const (
	WrapperNone WrapperKind = iota
	WrapperOptional
	WrapperNullable
)

func (s STNode[T]) Details() string {
	return fmt.Sprintf("%T %v", s.Concrete, s.Concrete)
}

func (s STExpr[T]) NewExpr(expr dst.Expr) Expr {
	return NewExpr(expr, s.pkg, s.file)
}

func buildComments(node dst.Node, genDecl *dst.GenDecl) string {
	var comments = BuildComments(node.Decorations())
	if len(comments) > 0 {
		return comments
	}
	if len(genDecl.Specs) > 1 {
		return ""
	}
	return BuildComments(genDecl.Decorations())
}

func NewFuncDecl(f *dst.FuncDecl, pkg *decorator.Package, file *dst.File) FuncDecl {
	return FuncDecl{
		NewNode(f, pkg, file),
	}
}

func NewVarConstDecl(node *dst.GenDecl, pkg *decorator.Package, file *dst.File) VarConstDecl {
	return VarConstDecl{
		STNode[*dst.GenDecl]{
			Concrete: node,
			file:     file,
			pkg:      pkg,
		},
	}
}

/**
 * STNode methods
 */

func (s STNode[T]) Node() dst.Node {
	return s.Concrete
}

func (s STNode[T]) File() *dst.File {
	return s.file
}

func (s STNode[T]) Pkg() *decorator.Package {
	return s.pkg
}

func (s STNode[T]) Pos() token.Pos {
	return s.pkg.Decorator.Map.Ast.Nodes[s.Concrete].Pos()
}

func (s STNode[T]) Position() token.Position {
	return s.pkg.Fset.Position(s.Pos())
}

func (s STNode[T]) Imports() ImportMap {
	return s.file.Imports
}

func NewNode[T dst.Node](node T, pkg *decorator.Package, file *dst.File) STNode[T] {
	return STNode[T]{
		Concrete: node,
		file:     file,
		pkg:      pkg,
	}
}

var _ Node = STNode[dst.Node]{}

/**
 * STExpr methods
 */

func (s STExpr[T]) Expr() dst.Expr {
	return s.Concrete
}

func NewExpr[T dst.Expr](expr T, pkg *decorator.Package, file *dst.File) STExpr[T] {
	return STExpr[T]{
		STNode: NewNode(expr, pkg, file),
	}
}

var _ Expr = STExpr[dst.Expr]{}

/**
 * CallExpr methods
 */

func NewCallExpr(ce *dst.CallExpr, pkg *decorator.Package, file *dst.File) CallExpr {
	return CallExpr{
		NewExpr(ce, pkg, file),
	}
}

func (c CallExpr) Args() []Expr {
	var args = make([]Expr, len(c.Concrete.Args))
	for i, arg := range c.Concrete.Args {
		args[i] = NewExpr(arg, c.pkg, c.file)
	}
	return args
}

func (e CallExpr) MustIdentifyFunc() TypeID {
	if t, ok := e.IdentifyFunc(); !ok {
		panic("expected type identifier for call expression")
	} else {
		return t
	}
}

func (e CallExpr) IdentifyFunc() (typeID TypeID, ok bool) {
	var expr dst.Expr
	switch _expr := e.Concrete.Fun.(type) {
	case *dst.IndexExpr:
		expr = _expr.X
	default:
		expr = _expr
	}
	switch t := expr.(type) {
	case *dst.SelectorExpr:
		var xIdent *dst.Ident
		if xIdent, ok = t.X.(*dst.Ident); !ok {
			return typeID, false
		}
		typeID.PkgPath, _ = e.Imports().GetPackageForPrefix(xIdent.Name)
		typeID.TypeName = t.Sel.Name
		return typeID, true
	case *dst.Ident:
		if t.Path == "" {
			typeID.PkgPath = e.Pkg().PkgPath
		} else {
			typeID.PkgPath = t.Path
		}
		typeID.TypeName = t.Name
		return typeID, true
	default:
		return TypeID{}, false
	}
}

/**
 * ValueSpec methods
 */

func NewValueSpec(genDecl *dst.GenDecl, node *dst.ValueSpec, pkg *decorator.Package, file *dst.File) ValueSpec {
	return ValueSpec{
		GenDecl: NewNode(genDecl, pkg, file),
		STNode:  NewNode(node, pkg, file),
	}
}

func (v ValueSpec) Comments() string {
	return buildComments(v.Concrete, v.GenDecl.Concrete)
}

func (v ValueSpec) HasType() bool {
	return v.Concrete.Type != nil
}

func (t ValueSpec) Type() dst.Expr {
	return t.Concrete.Type
}

func (t ValueSpec) Value() *dst.ValueSpec {
	return t.Concrete
}

/**
 * TypeSpec methods
 */

func NewTypeSpec(genDecl *dst.GenDecl, ts *dst.TypeSpec, pkg *decorator.Package, file *dst.File) TypeSpec {
	return TypeSpec{
		GenDecl: NewNode(genDecl, pkg, file),
		STNode:  NewNode(ts, pkg, file),
	}
}

func (t TypeSpec) Name() string {
	return t.Concrete.Name.Name
}

func (t TypeSpec) Derive() TypeExpr {

	return TypeExpr{TypeSpec: &t, Excerpt: t.Concrete.Type}
}

func (t TypeSpec) Comments() string {
	return buildComments(t.Concrete, t.GenDecl.Concrete)
}

func (t TypeSpec) ID() TypeID {
	return TypeID{PkgPath: t.pkg.PkgPath, TypeName: t.Concrete.Name.Name}
}

/**
 * TypeExpr methods
 */
func (t TypeExpr) Derive(expr dst.Expr) TypeExpr {
	return TypeExpr{TypeSpec: t.TypeSpec, Excerpt: expr}
}

func (t TypeExpr) Pos() token.Pos {
	return t.ToExpr().Pos()
}

func (t TypeExpr) ToExpr() Expr {
	return NewExpr(t.Excerpt, t.pkg, t.file)
}

func (t TypeExpr) Position() token.Position {
	return t.pkg.Fset.Position(t.Pos())
}

/**
 * StructType methods
 */

var NoStructType = StructType{}

func NewStructType(s *dst.StructType, t TypeSpec) StructType {
	if s == nil {
		panic("the struct type is nil")
	}
	return StructType{
		TypeSpec: t,
		Expr:     s,
	}
}

func (s StructType) Fields() (fields []StructField) {
	for _, _field := range s.Expr.Fields.List {
		field := StructField{TypeExpr: s.TypeSpec.Derive().Derive(_field.Type), Field: _field}
		if field.Skip() {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// Flatten resolves all type names into their actual shapes.
// It should recurse when a struct field is embedded, and then interleave
// the fields of the embedded type into the resulting struct.
// It has two helpers that are given as arguments:
// *resolve* is for when a type expression resolves to a named type.
// It will return an Expr representing the concrete type name, after
// looking up all type definition / aliasing, to arrive at the actual
// definition of the type.
// *seenProps* contains the names of all properties that have already
// been found on types that embed the present StructType.  If a field
// is encountered on the struct and found to exist in seenProps,
// it should be skipped.  Likewise, props that are considered to be
// "Skip()" by normal encoding/json rules, should be skipped here.
//
// ## Example:
// Given the following types:
// ```go
// type Foobar int
//
//	type ExampleStruct struct {
//	  Foobar []*Foobar `json:"foobar"`
//	}
//
//	type AnotherExample struct {
//	  Example  ExampleStruct `json:"example"`
//	  Example2 ExampleStruct `json:"example2"`
//	}
//
// ```
// The result of flattening it would look like:
// ```
//
//	struct {
//		Example struct {
//			Foobar int `json:"foobar"`
//		} `json:"example"`
//		Example2 struct {
//			Foobar []*int `json:"foobar"`
//		} `json:"example2"`
//	}
//
// ```
// In particular, note that the shape is exactly the same but the named
// types have all been resolved to their concrete type definitions.
func (s StructType) Flatten(
	localPkgPath string,
	resolve func(localPkgPath string, e IdentExpr) (Expr, error),
	seenProps SeenProps,
) (StructType, error) {
	newStruct := &dst.StructType{
		Fields: &dst.FieldList{},
	}
	var newFields []*dst.Field
	var embeddedFields []StructField
	var acceptedFieldNames = map[string]bool{}

	for _, fieldObj := range s.Fields() {
		if fieldObj.Skip() {
			continue
		}

		// embedded field => no explicit name
		if fieldObj.Embedded() {
			embeddedFields = append(embeddedFields, fieldObj)
			continue
		}
		var names []string
		for _, ident := range fieldObj.Field.Names {
			name := ident.Name
			if seenProps.Seen(name) {
				continue
			}
			acceptedFieldNames[name] = true
			seenProps = seenProps.See(name)
			names = append(names, name)
		}
		if len(names) == 0 {
			continue
		}
		flattenedType, err := flattenExpr(fieldObj.TypeAsExpr(), resolve, 0)
		if err != nil {
			return NoStructType, err
		}
		copied := dst.Clone(fieldObj.Field).(*dst.Field)
		copied.Names = nil
		for _, name := range names {
			copied.Names = append(copied.Names, dst.NewIdent(name))
		}
		copied.Type = flattenedType
		newFields = append(newFields, copied)
	}

	for _, fieldObj := range embeddedFields {
		embeddedExpr, err := flattenExpr(fieldObj.TypeAsExpr(), resolve, 0)
		if err != nil {
			return NoStructType, err
		}

		stNode, ok := embeddedExpr.(*dst.StructType)
		if !ok {
			// you said you want to error out for non-struct embedded fields
			return NoStructType, fmt.Errorf("embedded field must be struct type")
		}

		// dst.Print(stNode)

		// Recursively flatten the embedded struct, sharing the same seenProps
		subStruct := NewStructType(stNode, s.TypeSpec)
		flattened, err := subStruct.Flatten(localPkgPath, resolve, seenProps)
		if err != nil {
			return NoStructType, err
		}
		// Only keep property names that are not already in the parent struct
		// Also, if all names are already in the parent struct, skip the embedded struct

		// Child already handled skipping collisions. Just splice them in
		newFields = append(newFields, flattened.Expr.Fields.List...)
	}
	_ = acceptedFieldNames["foo"]

	newStruct.Fields.List = newFields
	flat := NewStructType(newStruct, s.TypeSpec)
	return flat, nil
}

type ExprResolveFunc func(localPkgPath string, e IdentExpr) (Expr, error)

// decorateIdentIfItPointsToInterface adds an "After" decoration to the ident
// if it points to an interface type.
// The point of this is to make sure that the ident is not flattened
// when it points to an interface type.
func decorateIdentIfItPointsToInterface(ident IdentExpr, resolve ExprResolveFunc) (IdentExpr, error) {
	var resolved, err = resolve(ident.Pkg().PkgPath, ident)
	if err != nil {
		return ident, err
	}
	if _ident, ok := resolved.Expr().(*dst.Ident); ok {
		newIdent := IdentExpr{STExpr: NewExpr(_ident, ident.pkg, ident.file)}
		newIdent, err = decorateIdentIfItPointsToInterface(newIdent, resolve)
		if err != nil {
			return ident, err
		}
		ident.InterfaceType = newIdent.InterfaceType
		return ident, nil
	}
	_, ident.InterfaceType = resolved.Expr().(*dst.InterfaceType)
	return ident, nil
}

func (s StructType) FlattenScan(localPkgPath string, scan ScanResult, seenProps SeenProps) (StructType, error) {
	return s.Flatten(localPkgPath, scan.resolveType, seenProps)
}

// flattenExpr recursively replaces any named type references with the
// result of `resolve(...)`, then continues descending into pointer/array types.
// It returns a final expression with no named references left (unless built-ins).
func flattenExpr(expr Expr, resolve ExprResolveFunc, depth int) (dst.Expr, error) {
	var result dst.Expr
	if depth > 50 {
		return nil, fmt.Errorf("flattenExpr recursion depth exceeded")
	}
	switch e := expr.Expr().(type) {
	case *dst.Ident:
		if isBuiltinOrBlank(e.Name) {
			return e, nil
		}
		identExpr := IdentExpr{
			STExpr: NewExpr(e, expr.Pkg(), expr.File()),
		}
		var err error
		identExpr, err = decorateIdentIfItPointsToInterface(identExpr, resolve)
		if err != nil {
			return nil, err
		}
		if identExpr.InterfaceType {
			return dst.Clone(e).(dst.Expr), nil
		}
		// Named type => must resolve
		resolved, err := resolve(expr.Pkg().PkgPath, identExpr)
		if err != nil {
			return nil, fmt.Errorf("flattenExpr from pkg %s %v: %w", expr.Pkg().PkgPath, e, err)
		}
		return flattenExpr(resolved, resolve, depth+1)

	case *dst.StarExpr:
		sub, err := flattenExpr(expr.NewExpr(e.X), resolve, depth+1)
		if err != nil {
			return nil, err
		}
		result = &dst.StarExpr{X: sub}

	case *dst.ArrayType:
		el, err := flattenExpr(expr.NewExpr(e.Elt), resolve, depth+1)
		if err != nil {
			return nil, err
		}
		var lenExpr dst.Expr
		if e.Len != nil {
			lenExpr = dst.Clone(e.Len).(dst.Expr)
		}

		result = &dst.ArrayType{
			Len: lenExpr,
			Elt: el,
		}

	case *dst.MapType:
		k, err := flattenExpr(expr.NewExpr(e.Key), resolve, depth+1)
		if err != nil {
			return nil, err
		}
		v, err := flattenExpr(expr.NewExpr(e.Value), resolve, depth+1)
		if err != nil {
			return nil, err
		}
		result = &dst.MapType{
			Key:   k,
			Value: v,
		}

	case *dst.StructType:
		// Flatten a literal struct
		copied := dst.Clone(e).(*dst.StructType)
		for _, field := range copied.Fields.List {
			typeExpr, err := flattenExpr(expr.NewExpr(field.Type), resolve, depth+1)
			if err != nil {
				return nil, err
			}
			field.Type = typeExpr
		}
		result = copied
	default:
		// Some other expression (func type, chan, etc.). We leave it as is.
		result = e
	}
	return dst.Clone(result).(dst.Expr), nil
}

// isBuiltinOrBlank returns true if the ident is `_` or one of the builtin names.
func isBuiltinOrBlank(name string) bool {
	if name == "_" {
		return true
	}
	switch name {
	case "bool", "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64", "complex64", "complex128":
		return true
	}
	return false
}

// SeenProps is a list of property names that are already "taken"
// in the flattened struct. If a property is seen, we skip it on any embedded struct.
type SeenProps []string

func (s SeenProps) Seen(t string) bool {
	return slices.Contains(s, t)
}

func (s SeenProps) See(t string) SeenProps {
	return append(SeenProps{t}, s...)
}

func (s SeenProps) Copy() SeenProps {
	return slices.Clone(s)
}

func (s SeenProps) Add(t string) (SeenProps, bool) {
	if s.Seen(t) {
		return nil, false
	}
	return s.See(t), true
}

/**
 * StructField methods
 */

func (f StructField) Comments() string {
	tag := f.structTag("description")
	if tag != nil {
		return tag.Value
	}

	return BuildComments(f.Field.Decorations())
}

func (f StructField) Embedded() bool {
	return len(f.Field.Names) == 0
}

func (f StructField) Type() dst.Expr {
	return f.Field.Type
}

func (f StructField) TypeAsExpr() Expr {
	if f.Field == nil {
		panic(fmt.Sprintf("Field is nil for %v", f))
	}
	return NewExpr(f.Field.Type, f.pkg, f.file)
}

func (f StructField) Required() bool {
	kind, _, _ := f.Wrapper()
	return kind != WrapperOptional
}

// Wrapper reports whether the complete field type is Optional[T] or
// Nullable[T] from this module's public package. Types with the same name from
// any other package are ordinary types.
func (f StructField) Wrapper() (WrapperKind, dst.Expr, error) {
	kind, args, ok := wrapperExpr(f.TypeAsExpr())
	if !ok {
		return WrapperNone, f.Type(), nil
	}
	if len(args) != 1 {
		return kind, nil, fmt.Errorf("%s requires exactly one type argument at %s", kind, f.Position())
	}
	return kind, args[0], nil
}

// HasJSONOption reports whether the field's encoding/json tag contains option.
func (f StructField) HasJSONOption(option string) bool {
	tag := f.JSONTag()
	return tag != nil && slices.Contains(tag.Options[1:], option)
}

func (k WrapperKind) String() string {
	switch k {
	case WrapperOptional:
		return "polytype.Optional"
	case WrapperNullable:
		return "polytype.Nullable"
	default:
		return "ordinary field"
	}
}

func wrapperExpr(expr Expr) (WrapperKind, []dst.Expr, bool) {
	var root dst.Expr
	var args []dst.Expr
	switch node := expr.Expr().(type) {
	case *dst.IndexExpr:
		root = node.X
		args = []dst.Expr{node.Index}
	case *dst.IndexListExpr:
		root = node.X
		args = node.Indices
	default:
		return WrapperNone, nil, false
	}

	var pkgPath, typeName string
	switch node := root.(type) {
	case *dst.Ident:
		pkgPath, typeName = node.Path, node.Name
	case *dst.SelectorExpr:
		prefix, ok := node.X.(*dst.Ident)
		if !ok {
			return WrapperNone, nil, false
		}
		pkgPath, typeName = prefix.Path, node.Sel.Name
		if pkgPath == "" {
			pkgPath, _ = expr.Imports().GetPackageForPrefix(prefix.Name)
		}
	default:
		return WrapperNone, nil, false
	}
	if pkgPath != SchemaPackagePath {
		return WrapperNone, nil, false
	}
	switch typeName {
	case "Optional":
		return WrapperOptional, args, true
	case "Nullable":
		return WrapperNullable, args, true
	default:
		return WrapperNone, nil, false
	}
}

func (f StructField) PropNames() (names []string) {
	switch len(f.Field.Names) {
	case 0:
		return
	case 1:
		if tag := f.JSONTag(); tag != nil && tag.Options[0] != "" {
			return []string{tag.Options[0]}
		}
	}
	for _, n := range f.Field.Names {
		if isExportedFieldName(n) {
			names = append(names, n.Name)
		}
	}
	return names
}

func (f StructField) structTag(name string) *structtag.Tag {
	return fieldStructTag(f.Field, name)
}

func fieldStructTag(field *dst.Field, name string) *structtag.Tag {
	if field.Tag == nil {
		return nil
	}
	tags, err := structtag.Parse(strings.Trim(field.Tag.Value, "`"))
	if err != nil {
		return nil
	}
	tag, err := tags.Get(name)
	if err != nil {
		return nil
	}
	if len(tag.Value) > 0 {
		return tag
	}
	return nil
}

func (f StructField) JSONTag() *structtag.Tag {
	return f.structTag("json")
}

func (f StructField) Skip() bool {
	// If there's a name list, check if all are unexported or check if `json:"-"`
	if len(f.Field.Names) > 0 {
		if !hasExportedFieldName(f.Field) {
			return true
		}
		return fieldJSONIgnored(f.Field)
	}
	// If embedded, do not skip unless it's unexported (i.e. an embedded private type).
	// For embedded types, check if the type is an ident with uppercase name, etc.
	if ident, ok := f.Field.Type.(*dst.Ident); ok {
		if !isExportedFieldName(ident) {
			return true
		}
	}
	return false
}

func isExportedFieldName(ident *dst.Ident) bool {
	return ident != nil && token.IsExported(ident.Name)
}

func hasExportedFieldName(field *dst.Field) bool {
	return slices.ContainsFunc(field.Names, isExportedFieldName)
}

func fieldJSONIgnored(field *dst.Field) bool {
	tag := fieldStructTag(field, "json")
	return tag != nil && len(tag.Options) > 0 && tag.Options[0] == "-"
}

/**
 * VarConstDecl methods
 */

func (v VarConstDecl) Specs() []ValueSpec {
	var specs = make([]ValueSpec, len(v.Concrete.Specs))
	for i, spec := range v.Concrete.Specs {
		specs[i] = ValueSpec{
			GenDecl: v.STNode,
			STNode:  NewNode(spec.(*dst.ValueSpec), v.pkg, v.file),
		}
	}
	return specs
}

/**
 * NamedType methods
 */

func (n TypeSpec) Type() Expr {
	return NewExpr(n.Concrete.Type, n.pkg, n.file)
}
