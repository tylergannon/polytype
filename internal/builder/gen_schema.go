package builder

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"hash/fnv"

	"slices"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/schema"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

//go:embed schemas.go.tmpl
var schemasTemplate string

const defaultSubdir = "jsonschema"

// DefaultDiscriminatorPropName is the discriminator property a sealed union
// uses when its interface declares none.
const DefaultDiscriminatorPropName = "type"
const unsupportedRegisteredInterfaceContainer = "arrays/slices of registered interfaces are not yet supported"

func New(pkg *decorator.Package) (SchemaBuilder, error) {
	return NewForTypes(pkg, nil)
}

// NewForLoad constructs a builder from a loaded package without lowering its
// roots. The result is suitable for callers that only need LowerRoots (the
// grammar path) and never render JSON Schema or Go codecs.
func NewForLoad(pkg *decorator.Package) (SchemaBuilder, error) {
	data, err := syntax.LoadPackage(pkg)
	if err != nil {
		return SchemaBuilder{}, err
	}
	return newFromScan(data, nil, false, noSchemas)
}

// NewForTypes constructs a builder for the selected registered schema roots.
// An empty selection preserves New's behavior and walks every registered root:
// a root declared with a schema entrypoint gets JSON Schema, and a root
// declared without one (Declare[T]()) gets only the outputs that need none.
func NewForTypes(pkg *decorator.Package, typeNames []string) (SchemaBuilder, error) {
	data, err := syntax.LoadPackage(pkg)
	if err != nil {
		return SchemaBuilder{}, err
	}
	return newFromScan(data, typeNames, true, entrypointSchemas)
}

// schemaSelection says which roots get a JSON Schema.
type schemaSelection int

const (
	// noSchemas maps no schema: grammar-only and codec-only runs.
	noSchemas schemaSelection = iota
	// entrypointSchemas maps a schema for each root that declares a schema
	// entrypoint. A declaration file selects outputs per root this way.
	entrypointSchemas
	// allSchemas maps a schema for every root: a programmatic run that
	// selects JSON Schema.
	allSchemas
)

// hasSchema reports whether root gets a JSON Schema.
func (s SchemaBuilder) hasSchema(root syntax.SchemaMethod) bool {
	switch s.schemaRoots {
	case allSchemas:
		return true
	case entrypointSchemas:
		return root.SchemaMethodName != ""
	default:
		return false
	}
}

// roots returns every declared root, method entrypoints first.
func (s SchemaBuilder) roots() []syntax.SchemaMethod {
	roots := slices.Clone(s.Scan.SchemaMethods)
	for _, fn := range s.Scan.SchemaFuncs {
		roots = append(roots, syntax.SchemaMethod(fn))
	}
	return roots
}

func newFromScan(data syntax.ScanResult, typeNames []string, discoverCodecs bool, schemas schemaSelection) (SchemaBuilder, error) {
	var builder = SchemaBuilder{
		Scan:              data,
		schemaRoots:       schemas,
		Subdir:            defaultSubdir,
		BuildTag:          syntax.BuildTag,
		DiscriminatorProp: DefaultDiscriminatorPropName,
		TypeProvidersMap:  map[string][]FieldProvider{},
		EnumV1:            make(map[string]map[string]enumFieldConfig),
		RenderedTypes:     []string{},
		Rendered:          map[string]bool{},
		RefTypes:          map[syntax.TypeID]bool{},
		schemas:           map[string]schema.JSONSchema{},
		ownerCodecs:       map[string]OwnerCodec{},
	}
	// First, collect providers so they're available during lowering
	collectOpts := func(recv syntax.TypeID, opts []syntax.SchemaMethodOptionInfo) {
		if len(opts) == 0 {
			return
		}
		recvName := recv.TypeName
		for _, opt := range opts {
			switch string(opt.Kind) {
			case "WithRenderProviders":
				builder.RenderedTypes = append(builder.RenderedTypes, recvName)
				builder.Rendered[recvName] = true
				continue
			case "AsRef":
				// recv already carries the receiver's actual resolved PkgPath
				// (the scanner resolves foreign selector-expression receivers,
				// e.g. otherpkg.Shared.Schema, to otherpkg's real import path),
				// so distinct types sharing a bare name are kept distinct here.
				builder.RefTypes[recv.Concrete()] = true
				continue
			case "WithStringerEnum":
				// Enum options don't create providers, they're handled inline
				continue
			}
			builder.TypeProvidersMap[recvName] = append(builder.TypeProvidersMap[recvName], FieldProvider{
				FieldName:        opt.FieldName,
				Kind:             string(opt.Kind),
				ProviderName:     opt.ProviderName,
				ProviderIsMethod: opt.ProviderIsMethod,
			})
		}
	}
	for _, m := range data.SchemaMethods {
		collectOpts(m.Receiver, m.Options)
	}
	for _, f := range data.SchemaFuncs {
		collectOpts(f.Receiver, f.Options)
	}

	// Collect enum options per receiver/field
	applyEnumOpts := func(recv string, opts []syntax.SchemaMethodOptionInfo) error {
		for _, opt := range opts {
			if opt.Kind != "WithStringerEnum" {
				continue
			}
			if builder.EnumV1[recv] == nil {
				builder.EnumV1[recv] = make(map[string]enumFieldConfig)
			}
			if _, ok := builder.EnumV1[recv][opt.FieldName]; ok {
				return fmt.Errorf("field %s.%s: duplicate enum registration", recv, opt.FieldName)
			}
			builder.EnumV1[recv][opt.FieldName] = enumFieldConfig{UseStringer: true}
		}
		return nil
	}
	for _, m := range data.SchemaMethods {
		if err := applyEnumOpts(m.Receiver.TypeName, m.Options); err != nil {
			return builder, err
		}
	}
	for _, f := range data.SchemaFuncs {
		if err := applyEnumOpts(f.Receiver.TypeName, f.Options); err != nil {
			return builder, err
		}
	}

	// Build TypeProviders slice for template convenience, computing JSON names for fields

	for typeName, providers := range builder.TypeProvidersMap {
		// compute json names from type spec
		if ts, ok := builder.Scan.LocalNamedTypes[typeName]; ok {
			if st, ok2 := ts.Type().Expr().(*dst.StructType); ok2 {
				stWrap := syntax.NewStructType(st, ts)
				for i := range providers {
					for _, f := range stWrap.Fields() {
						for _, name := range f.Field.Names {
							if name.Name == providers[i].FieldName {
								jsonNames := f.PropNames()
								if len(jsonNames) > 0 {
									providers[i].JSONName = jsonNames[0]
								}
							}
						}
					}
				}
			}
		}
		builder.TypeProviders = append(builder.TypeProviders, TypeProviders{TypeName: typeName, Providers: providers})
	}
	if !discoverCodecs && schemas == noSchemas {
		// Grammar-only callers lower their own roots through LowerRoots.
		return builder, nil
	}
	// One lowering serves every output. JSON Schema is projected from it
	// for the roots that get one, and the Go codec plans are derived from it
	// for every root.
	lowered, err := builder.lower(typeNames)
	if err != nil {
		return builder, err
	}
	builder.lowered = lowered
	builder.selectedRoots = typeNames
	if err := builder.projectSchemas(); err != nil {
		return builder, err
	}
	if builder.ownerCodecs, err = planOwnerCodecs(&builder, lowered); err != nil {
		return builder, err
	}
	if err := builder.validateOwnerCodecMethods(); err != nil {
		return builder, err
	}
	if err := builder.validateEnumCodecMethods(); err != nil {
		return builder, err
	}
	return builder, nil
}

// projectSchemas renders the JSON Schema of every root that gets one. Every
// root shares one "$defs" namespace, so a name collision between two AsRef
// types is found here, before anything is written.
func (s *SchemaBuilder) projectSchemas() error {
	var (
		roots   []schema.Root
		methods []syntax.SchemaMethod
	)
	for _, root := range s.lowered.roots {
		if !s.hasSchema(root.method) {
			continue
		}
		roots = append(roots, root.schema)
		methods = append(methods, root.method)
		s.GenerateSchemas = s.GenerateSchemas || root.method.SchemaMethodName != ""
	}
	refs := make(map[typegrammar.Name]bool, len(s.RefTypes))
	for id := range s.RefTypes {
		refs[typegrammar.Name{PackagePath: id.PkgPath, Name: id.TypeName}] = true
	}
	schemas, err := schema.Generate(s.lowered.defs, roots, schema.Options{Refs: refs})
	if err != nil {
		return s.schemaError(err)
	}
	for i, method := range methods {
		s.schemas[method.Receiver.TypeName] = schemas[i]
	}
	return nil
}

// schemaError words a projection failure for the CLI. A recursive type is
// reported once, naming the package as the source names it.
func (s *SchemaBuilder) schemaError(err error) error {
	recursive, ok := errors.AsType[*schema.RecursionError](err)
	if !ok {
		return err
	}
	pkgName := recursive.Type.PackagePath
	if scan, ok := s.Scan.GetPackage(recursive.Type.PackagePath); ok {
		pkgName = scan.Pkg.Name
	}
	return &recursiveSchemaError{
		root:     recursive.Root.TypeName(),
		typeName: pkgName + "." + recursive.Type.Name,
		position: recursive.Source,
	}
}

// recursiveSchemaError reports a type that contains itself, found while
// projecting a root's JSON Schema. A schema inlines every type it references,
// so it cannot express the cycle; the other outputs are unaffected.
type recursiveSchemaError struct {
	root     string
	typeName string
	position token.Position
}

func (e *recursiveSchemaError) Error() string {
	return fmt.Sprintf("JSON Schema cannot express the recursive type %s (%s), reached from root %s: a schema inlines types, and this type contains itself. "+
		"Only the JSON Schema output has this limit; Go JSON codecs, TypeScript and devalue support recursive types and are unaffected. "+
		"If you do not need a schema for %s, select codegen.GoJSON() instead of codegen.JSONSchema(), or in a declaration file declare it without a schema entrypoint: polytype.Declare[%s]()",
		e.typeName, e.position, e.root, e.root, e.root)
}

func selectedRoot(typeNames []string, candidate string) bool {
	return len(typeNames) == 0 || slices.Contains(typeNames, candidate)
}

type enumFieldConfig struct {
	UseStringer bool
}

type InterfaceOptionInfo struct {
	TypeNameWithPrefix string
	Discriminator      string
	Pointer            bool
}

type FieldProvider struct {
	FieldName        string
	JSONName         string
	Kind             string
	ProviderName     string
	ProviderIsMethod bool
}

type TypeProviders struct {
	TypeName  string
	IsPointer bool
	Providers []FieldProvider
}

type InterfaceInfo struct {
	TypeNameWithPrefix    string
	TypeName              string
	MarshalerFunc         string
	UnmarshalerFunc       string
	DiscriminatorPropName string
	Options               []InterfaceOptionInfo
}

type SchemaBuilder struct {
	Scan              syntax.ScanResult
	Subdir            string
	Pretty            bool
	Validate          bool
	BuildTag          string
	DiscriminatorProp string
	// GenerateSchemas controls schema accessors and embedding in generated Go
	// code. It is set when some root with a schema entrypoint gets a schema;
	// codec-only generation leaves it false and embeds no schema assets.
	GenerateSchemas bool
	schemaRoots     schemaSelection

	// Field provider options per type (by receiver type name)
	TypeProvidersMap map[string][]FieldProvider
	TypeProviders    []TypeProviders

	// Enum options: receiver -> field -> config
	EnumV1 map[string]map[string]enumFieldConfig

	// Types requesting rendered provider execution
	RenderedTypes []string
	Rendered      map[string]bool

	// Types requesting AsRef(): rendered as "$ref" into "$defs" wherever referenced.
	RefTypes map[syntax.TypeID]bool

	// lowered is the one lowering of the selected roots that every output is
	// projected from; nil until newFromScan lowers, or for a grammar-only
	// builder.
	lowered       *lowering
	selectedRoots []string
	// schemas holds each schema root's rendered JSON Schema by type name.
	schemas map[string]schema.JSONSchema
	// ownerCodecs holds the generated owner codec plan by type name.
	ownerCodecs map[string]OwnerCodec
}

// HasGeneratedJSONCode reports whether the configured roots require enum or
// owner codecs. A GoJSON-only request for plain structs therefore performs no
// write instead of emitting an otherwise empty generated file.
func (s SchemaBuilder) HasGeneratedJSONCode() bool {
	return len(s.enumMarkers()) > 0 || len(s.sortedOwnerCodecNames()) > 0
}

// schemaTemplateData is the complete input of renderGoCode: every value the
// generated Go file is rendered from, resolved ahead of rendering so that the
// template consults no scan, package graph or filesystem. A test builds one
// by hand and asserts on the rendered code without loading a package.
type schemaTemplateData struct {
	// PackageName is the package clause of the generated file.
	PackageName string
	// BuildTag is the constraint the generated file is excluded from
	// (//go:build !BuildTag), so it never compiles beside the declarations.
	BuildTag string
	// Subdir is the embedded schema directory, relative to the package.
	Subdir string
	// Validate emits ValidateJSON and the compiled schemas behind it.
	Validate bool
	// GenerateSchemas emits the embed.FS and the schema accessors; codec-only
	// output leaves it false.
	GenerateSchemas bool
	// DiscriminatorProp is the package's default discriminator property, used
	// by every union helper whose interface declared none of its own.
	DiscriminatorProp string
	// Imports are the import specs the codecs need beyond the standard
	// library, already aliased and quoted.
	Imports []string
	// SchemaMethods are the schema entrypoints generated as methods;
	// SchemaFreeFuncs those whose receiver type cannot carry a method.
	SchemaMethods   []SchemaAccessor
	SchemaFreeFuncs []SchemaAccessor
	// Rendered marks the roots whose schema is a provider template and
	// RenderedTypes lists them: they get RenderedSchema, not ValidateJSON.
	Rendered      map[string]bool
	RenderedTypes []string
	// TypeProviders are the provider registrations RenderedSchema calls.
	TypeProviders []TypeProviders
	OwnerCodecs   []OwnerCodec
	Interfaces    []InterfaceInfo
	// EnumMarkers lists every type in the generated package that declares
	// the func (T) enum() marker, sorted by type name. The template emits one
	// interface assertion per type, assigning the type's first typed constant,
	// so the marker is referenced from production code: that keeps its shape
	// checked at compile time and satisfies the staticcheck unused-method
	// check without any lint directives.
	EnumMarkers []EnumMarker
}

// SchemaAccessor is one generated schema entrypoint: the method (or, when Go
// forbids a method on the receiver type, the free function) MethodName that
// returns TypeName's embedded schema.
type SchemaAccessor struct {
	TypeName   string
	MethodName string
	Pointer    bool
}

func schemaAccessors(methods []syntax.SchemaMethod) []SchemaAccessor {
	out := make([]SchemaAccessor, 0, len(methods))
	for _, m := range methods {
		out = append(out, SchemaAccessor{
			TypeName:   m.Receiver.TypeName,
			MethodName: m.SchemaMethodName,
			Pointer:    m.IsPointer(),
		})
	}
	return out
}

// EnumMarker is one enum-marked type in the generated package together with
// the name of its first typed constant in declaration order. The assertion
// assigns a value of the type rather than a pointer because the marker uses
// a value receiver.
type EnumMarker struct {
	TypeName string
	Constant string
	// Underlying is the spelling of the type's underlying basic type
	// ("string", "int", "uint8", ...). It is the decode target and the
	// conversion used when an error reports a rejected value.
	Underlying string
	// IsString distinguishes the two admitted wire forms. A string enum is
	// reported with %q in errors, an integer enum with %v.
	IsString bool
	// Members are the type's constants in declaration order, deduplicated
	// by wire value: two constants sharing a value would be a duplicate
	// case in the generated switch, and the first name wins.
	// Empty when the type's underlying type is neither string nor integer,
	// which is rejected only if a schema actually reaches the type; no
	// codec is emitted in that case.
	Members []EnumMember
}

// EnumMember is one enum constant together with the exact JSON text it
// encodes to. Wire is JSON, not Go: the template quotes it for Go with %q.
type EnumMember struct {
	Constant string
	Wire     string
}

func (schemaTemplateData) GeneratesJSONUnmarshalers() bool {
	return true
}

// HasNonRenderedTypes reports whether at least one schema method is for a
// non-rendered type, which is what ValidateJSON can be generated for.
func (s schemaTemplateData) HasNonRenderedTypes() bool {
	for _, m := range s.SchemaMethods {
		if !s.Rendered[m.TypeName] {
			return true
		}
	}
	return false
}

func (s schemaTemplateData) HaveInterfaces() bool {
	return len(s.Interfaces) > 0
}

func (s schemaTemplateData) HaveEnumCodecs() bool {
	return slices.ContainsFunc(s.OwnerCodecs, func(owner OwnerCodec) bool { return len(owner.EnumFields) > 0 })
}

func (s schemaTemplateData) UsesJSONV2Marshal() bool {
	return s.GeneratesJSONUnmarshalers() && (len(s.OwnerCodecs) > 0 || len(s.Interfaces) > 0)
}

func (s SchemaBuilder) validateOwnerCodecMethods() error {
	owners := s.sortedOwnerCodecNames()
	for _, owner := range owners {
		foreignEmbedded, err := s.findForeignEmbeddedGeneratedCodec(owner)
		if err != nil {
			return err
		}
		if foreignEmbedded != "" {
			return fmt.Errorf(
				"cannot generate owner codec for %s: foreign embedded type %s has generated production JSON codecs and would promote a competing MarshalJSON",
				owner,
				foreignEmbedded,
			)
		}
		embeddedOwner, err := s.findEmbeddedOwnerCodec(owner, map[string]bool{})
		if err != nil {
			return err
		}
		if embeddedOwner != "" {
			return fmt.Errorf(
				"cannot generate owner codec for %s: embedded type %s also requires generated owner codecs and would promote a competing MarshalJSON",
				owner,
				embeddedOwner,
			)
		}
	}
	methods, err := syntax.FindProductionJSONMethods(s.Scan.Pkg.Dir, nil)
	if err != nil {
		return fmt.Errorf("discovering production JSON methods: %w", err)
	}
	for _, owner := range owners {
		embedded, err := s.embeddedTypeNames(owner, map[string]bool{})
		if err != nil {
			return err
		}
		for _, method := range methods {
			if method.Receiver == owner || embedded[method.Receiver] {
				return ownerCodecCollision(owner, method.Name, method.Position)
			}
		}
	}
	for _, owner := range owners {
		object := s.Scan.Pkg.Types.Scope().Lookup(owner)
		if object == nil {
			return fmt.Errorf("cannot resolve owner codec type %s", owner)
		}
		methodSet := types.NewMethodSet(types.NewPointer(object.Type()))
		for _, methodName := range []string{"MarshalJSON", "UnmarshalJSON"} {
			selection := methodSet.Lookup(nil, methodName)
			if selection == nil {
				continue
			}
			position := s.Scan.Pkg.Fset.Position(selection.Obj().Pos())
			active, err := syntax.IsProductionGoFile(position.Filename)
			if err != nil {
				return fmt.Errorf("checking production build constraint for %s: %w", position, err)
			}
			if active {
				return ownerCodecCollision(owner, methodName, position)
			}
		}
	}
	return nil
}

// validateEnumCodecMethods rejects, before anything is written, a generation
// target whose production code already declares MarshalJSON or UnmarshalJSON
// on an enum-marked type that would receive a generated type-level codec.
// Emitting the codec anyway leaves the package with a duplicate method
// declaration, so the collision is reported the way an owner codec collision
// is: as a pre-write diagnostic naming the type, the method and its position.
func (s *SchemaBuilder) validateEnumCodecMethods() error {
	var receivers []string
	for _, marker := range s.enumMarkers() {
		if len(marker.Members) > 0 {
			receivers = append(receivers, marker.TypeName)
		}
	}
	if len(receivers) == 0 {
		return nil
	}
	methods, err := syntax.FindProductionJSONMethods(s.Scan.Pkg.Dir, receivers)
	if err != nil {
		return fmt.Errorf("discovering production JSON methods: %w", err)
	}
	if len(methods) > 0 {
		return fmt.Errorf(
			"cannot generate enum codec for %s: handwritten production %s already declared at %s",
			methods[0].Receiver,
			methods[0].Name,
			methods[0].Position,
		)
	}
	return nil
}

func (s SchemaBuilder) findForeignEmbeddedGeneratedCodec(owner string) (string, error) {
	typeSpec, ok := s.Scan.LocalNamedTypes[owner]
	if !ok {
		return "", nil
	}
	structExpr, ok := typeSpec.Type().Expr().(*dst.StructType)
	if !ok {
		return "", nil
	}
	return s.findForeignEmbeddedGeneratedCodecIn(
		syntax.NewStructType(structExpr, typeSpec),
		map[syntax.TypeID]bool{},
	)
}

func (s SchemaBuilder) findForeignEmbeddedGeneratedCodecIn(current syntax.StructType, seen map[syntax.TypeID]bool) (string, error) {
	if seen[current.ID()] {
		return "", nil
	}
	seen[current.ID()] = true
	for _, field := range current.Fields() {
		if !field.Embedded() {
			continue
		}
		embedded, err := s.resolveEmbeddedType(field.TypeExpr, nil)
		if err != nil {
			return "", err
		}
		if embedded.Pkg().PkgPath != s.Scan.Pkg.PkgPath {
			methods, err := syntax.FindGeneratedJSONMethods(embedded.Pkg().Dir, []string{embedded.Name()})
			if err != nil {
				return "", fmt.Errorf("discovering generated JSON methods for embedded type %s.%s: %w", embedded.Pkg().Name, embedded.Name(), err)
			}
			if len(methods) > 0 {
				return embedded.Pkg().Name + "." + embedded.Name(), nil
			}
		}
		found, err := s.findForeignEmbeddedGeneratedCodecIn(embedded, seen)
		if err != nil || found != "" {
			return found, err
		}
	}
	return "", nil
}

func (s SchemaBuilder) findEmbeddedOwnerCodec(owner string, seen map[string]bool) (string, error) {
	embedded, err := s.embeddedTypeNames(owner, seen)
	if err != nil {
		return "", err
	}
	for _, candidate := range s.sortedOwnerCodecNames() {
		if embedded[candidate] {
			return candidate, nil
		}
	}
	return "", nil
}

func (s SchemaBuilder) embeddedTypeNames(owner string, seen map[string]bool) (map[string]bool, error) {
	result := map[string]bool{}
	if seen[owner] {
		return result, nil
	}
	seen[owner] = true
	typeSpec, ok := s.Scan.LocalNamedTypes[owner]
	if !ok {
		return result, nil
	}
	structExpr, ok := typeSpec.Type().Expr().(*dst.StructType)
	if !ok {
		return result, nil
	}
	for _, field := range syntax.NewStructType(structExpr, typeSpec).Fields() {
		if !field.Embedded() {
			continue
		}
		embedded, err := s.resolveEmbeddedType(field.TypeExpr, nil)
		if err != nil {
			return nil, err
		}
		if embedded.Pkg().PkgPath != s.Scan.Pkg.PkgPath {
			continue
		}
		result[embedded.Name()] = true
		nested, err := s.embeddedTypeNames(embedded.Name(), seen)
		if err != nil {
			return nil, err
		}
		for name := range nested {
			result[name] = true
		}
	}
	return result, nil
}

func ownerCodecCollision(owner, method string, position token.Position) error {
	return fmt.Errorf(
		"cannot generate owner codec for %s: handwritten production %s already declared or promoted at %s",
		owner,
		method,
		position,
	)
}

// imports lists every package the generated codecs name: each union's
// interface and variants, and each adapted enum's type.
func (s SchemaBuilder) imports() *ImportMap {
	importMap := NewImportMap(s.Scan.Pkg)
	for _, owner := range s.ownerCodecs {
		for _, prop := range owner.UnionFields {
			importMap.AddPackage(s.packageOf(prop.Union.Interface.PackagePath))
			for _, variant := range prop.Union.Variants {
				importMap.AddPackage(s.packageOf(variant.Implementation.PackagePath))
			}
		}
		for _, field := range owner.EnumFields {
			importMap.AddPackage(s.packageOf(field.EnumType.PackagePath))
		}
	}
	return importMap
}

// packageOf returns a loaded package by import path. Every path reachable
// from a lowered definition was loaded to lower it.
func (s SchemaBuilder) packageOf(pkgPath string) *decorator.Package {
	scan, ok := s.Scan.GetPackage(pkgPath)
	if !ok {
		panic("internal error: no package found for " + pkgPath)
	}
	return scan.Pkg
}

// hasInvalidMethodReceiverBase reports whether typeName's underlying type is
// itself a pointer or interface, meaning Go forbids declaring any method
// (value or pointer receiver) on it.
func (s SchemaBuilder) hasInvalidMethodReceiverBase(typeName string) bool {
	// A type registered via NewInterfaceImpl/WithInterfaceImpls is recorded
	// in Scan.Interfaces, not Scan.LocalNamedTypes (see scan_result.go's
	// type-decl pass), but it's still an interface: no method can be
	// declared on it either.
	if _, ok := s.Scan.Interfaces[typeName]; ok {
		return true
	}
	// Resolve through go/types rather than pattern-matching the immediate
	// declaration AST: a forwarding definition (type Q P, where P is
	// itself a pointer or interface) has an *dst.Ident, not a *dst.StarExpr
	// or *dst.InterfaceType, as its own declaration expression, but Go
	// still resolves Q's underlying type to a pointer/interface and
	// forbids a method on it exactly the same as a direct declaration.
	obj := s.Scan.Pkg.Types.Scope().Lookup(typeName)
	if obj == nil {
		return false
	}
	switch obj.Type().Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}

// SchemaMethods returns registered schema entrypoints that can be generated
// as a Go method on their receiver type: true method-root registrations,
// plus free-function-root registrations (SchemaFuncs) whose receiver type
// can legally have a method declared on it. Entries with an invalid
// receiver base type are dropped here regardless of source, matching prior
// behavior for method-root registrations (which should never have an
// invalid base in a package that actually compiles).
func (s SchemaBuilder) SchemaMethods() []syntax.SchemaMethod {
	var out []syntax.SchemaMethod
	for _, m := range s.Scan.SchemaMethods {
		if !s.hasInvalidMethodReceiverBase(m.Receiver.TypeName) {
			out = append(out, m)
		}
	}
	for _, f := range s.Scan.SchemaFuncs {
		if !s.hasInvalidMethodReceiverBase(f.Receiver.TypeName) {
			out = append(out, syntax.SchemaMethod(f))
		}
	}
	return out
}

// SchemaFreeFuncs returns free-function-root registrations whose receiver
// type's underlying type is a pointer or interface, so they must be
// generated as a free function (matching the original registration's
// signature) rather than a method.
// isBuilderMarker reports whether fn's registration was NewJSONSchemaBuilder,
// whose stub takes no arguments (func() json.RawMessage) -- unlike
// NewJSONSchemaFunc/fluent Declare's free-function form, whose stub takes
// the receiver type as its sole argument (func(T) json.RawMessage). Both
// land in Scan.SchemaFuncs, but they aren't interchangeable: emitting the
// one-argument free-function shape for a builder registration would change
// its signature and break callers (or collide if the same builder function
// is reused for two invalid-receiver types).
func isBuilderMarker(fn syntax.SchemaFunction) bool {
	id, ok := fn.MarkerCall.CallExpr.IdentifyFunc()
	return ok && id.TypeName == syntax.MarkerFuncNewJSONSchemaBuilder
}

// SchemaFreeFuncs returns free-function-root registrations (NewJSONSchemaFunc
// or fluent Declare with a free function) whose receiver type's underlying
// type is a pointer or interface, so they must be generated as a free
// function (matching the original registration's signature) rather than a
// method. NewJSONSchemaBuilder registrations are excluded even when they'd
// otherwise qualify: see InvalidReceiverBuilderRoots.
func (s SchemaBuilder) SchemaFreeFuncs() []syntax.SchemaMethod {
	var out []syntax.SchemaMethod
	for _, f := range s.Scan.SchemaFuncs {
		if isBuilderMarker(f) {
			continue
		}
		if s.hasInvalidMethodReceiverBase(f.Receiver.TypeName) {
			out = append(out, syntax.SchemaMethod(f))
		}
	}
	return out
}

// InvalidReceiverBuilderRoots returns NewJSONSchemaBuilder registrations
// whose receiver type's underlying type is a pointer or interface. Go
// forbids a method there, and the builder's zero-argument stub signature
// can't be preserved as a free function without risking a name collision
// (the same builder function reused for two such types), so generation
// must reject this combination rather than silently drop or miscompile it.
func (s SchemaBuilder) InvalidReceiverBuilderRoots() []syntax.SchemaMethod {
	var out []syntax.SchemaMethod
	for _, f := range s.Scan.SchemaFuncs {
		if isBuilderMarker(f) && s.hasInvalidMethodReceiverBase(f.Receiver.TypeName) {
			out = append(out, syntax.SchemaMethod(f))
		}
	}
	return out
}

// loadScanResult gets the scan result associated with the given syntax.TypeID
func (s SchemaBuilder) loadScanResult(t syntax.TypeID) (syntax.ScanResult, error) {
	if t.PkgPath == "" {
		return syntax.ScanResult{}, fmt.Errorf("empty package path in loadScanResult")
	}
	if res, ok := s.Scan.GetPackage(t.PkgPath); ok {
		return res, nil
	}
	return syntax.ScanResult{}, fmt.Errorf("package was not loaded: %s", t.PkgPath)
}

func (s SchemaBuilder) find(t syntax.TypeID) (token.Position, error) {
	sb, err := s.loadScanResult(t)
	if err != nil {
		return token.Position{}, err
	}
	typeSpec, ok := sb.LocalNamedTypes[t.TypeName]
	if !ok {
		return token.Position{}, fmt.Errorf("SchemaBuilder.find: type %s not found", t.TypeName)
	}
	return typeSpec.Position(), nil
}

func (s SchemaBuilder) schemaArtifactName(t syntax.TypeID) string {
	if _, templated := s.TypeProvidersMap[t.TypeName]; templated {
		return fmt.Sprintf("%s.json.tmpl", t.TypeName)
	}
	return fmt.Sprintf("%s.json", t.TypeName)
}

func (s SchemaBuilder) writeSchema(t syntax.TypeID, targetDir string, noChanges bool) (wroteNew bool, err error) {
	var (
		ok       bool
		filePath string
		sumPath  string
		tmpFile  *os.File
	)

	filePath = filepath.Join(targetDir, s.schemaArtifactName(t))
	sumPath = filePath + ".sum"

	// Create temp file in same directory to ensure same filesystem

	if tmpFile, err = os.CreateTemp(targetDir, fmt.Sprintf("%s.*.json.tmp", t.TypeName)); err != nil {
		return false, fmt.Errorf("could not create temp file: %w", err)
	}
	defer func() {
		if fCloseErr := tmpFile.Close(); fCloseErr != nil && !errors.Is(fCloseErr, os.ErrClosed) {
			err = errors.Join(err, fmt.Errorf("could not close temp file: %w", fCloseErr))
		}
		// Clean up temp file if we're returning with an error or if we didn't use it
		_, statErr := os.Stat(tmpFile.Name())
		if os.IsNotExist(statErr) {
			return
		} else if statErr != nil {
			err = errors.Join(err, fmt.Errorf("could not stat temp file: %w", statErr))
			return
		}
		if rmErr := os.Remove(tmpFile.Name()); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("could not remove temp file: %w", rmErr))
		}
	}()

	rootSchema, ok := s.schemas[t.TypeName]
	if !ok {
		return false, fmt.Errorf("unknown type %s", t)
	}

	hash := fnv.New64a()
	writer := io.MultiWriter(tmpFile, hash)
	_, templated := s.TypeProvidersMap[t.TypeName]
	// Templates cannot use the standard pretty encoder because their holes are
	// not valid JSON. Preserve the existing raw output when pretty is requested.
	if templated && s.Pretty {
		var b []byte
		if b, err = rootSchema.MarshalJSON(); err != nil {
			return false, fmt.Errorf("could not marshal template schema: %w", err)
		}
		if _, err = writer.Write(b); err != nil {
			return false, fmt.Errorf("could not write template schema: %w", err)
		}
		if _, err = writer.Write([]byte("\n")); err != nil {
			return false, fmt.Errorf("could not write newline: %w", err)
		}
	} else if s.Pretty {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		if err = encoder.Encode(rootSchema); err != nil {
			return false, fmt.Errorf("could not encode schema: %w", err)
		}
	} else {
		var b []byte
		if b, err = schema.MarshalHardlines(rootSchema); err != nil {
			return false, fmt.Errorf("could not format schema: %w", err)
		}
		if _, err = writer.Write(b); err != nil {
			return false, fmt.Errorf("could not write schema: %w", err)
		}
		if _, err = writer.Write([]byte("\n")); err != nil {
			return false, fmt.Errorf("could not write newline: %w", err)
		}
	}

	newChecksum := hex.EncodeToString(hash.Sum(nil))

	// Check if content actually changed by comparing with old checksum
	wroteNew = true
	if oldSum, err := os.ReadFile(sumPath); err == nil {
		wroteNew = string(oldSum) != newChecksum
	}

	// If content changed and we're in noChanges mode, return without writing anything
	if wroteNew && noChanges {
		return true, nil
	}

	// Move temp file into place and write new checksum
	if err = tmpFile.Close(); err != nil {
		return false, fmt.Errorf("could not close temp file: %w", err)
	}
	if err = os.Rename(tmpFile.Name(), filePath); err != nil {
		return false, fmt.Errorf("could not move temp file into place: %w", err)
	}
	if err = os.WriteFile(sumPath, []byte(newChecksum), 0644); err != nil {
		return false, fmt.Errorf("could not write checksum file: %w", err)
	}

	return wroteNew, nil
}

func pruneOrphanedSchemaArtifacts(targetDir string, expected map[string]bool, noChanges bool) ([]string, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, fmt.Errorf("could not inspect generated schema directory: %w", err)
	}

	var orphaned []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sum") || expected[name] {
			continue
		}
		artifactName := strings.TrimSuffix(name, ".sum")
		if !strings.HasSuffix(artifactName, ".json") && !strings.HasSuffix(artifactName, ".json.tmpl") {
			continue
		}

		sumPath := filepath.Join(targetDir, name)
		sumData, readErr := os.ReadFile(sumPath)
		if readErr != nil {
			return nil, fmt.Errorf("could not read orphaned schema checksum %s: %w", name, readErr)
		}

		artifactPath := filepath.Join(targetDir, artifactName)
		artifactData, readErr := os.ReadFile(artifactPath)
		switch {
		case readErr == nil:
			hash := fnv.New64a()
			_, _ = hash.Write(artifactData)
			actualChecksum := hex.EncodeToString(hash.Sum(nil))
			if strings.TrimSpace(string(sumData)) != actualChecksum {
				return nil, fmt.Errorf("refusing to remove modified orphaned schema %s because it does not match %s", artifactName, name)
			}
			orphaned = append(orphaned, artifactName)
		case errors.Is(readErr, os.ErrNotExist):
			// A checksum without its generated artifact is itself stale.
		default:
			return nil, fmt.Errorf("could not read orphaned schema %s: %w", artifactName, readErr)
		}
		orphaned = append(orphaned, name)
	}

	slices.Sort(orphaned)
	if noChanges {
		return orphaned, nil
	}
	for _, name := range orphaned {
		if err := os.Remove(filepath.Join(targetDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not remove orphaned generated schema artifact %s: %w", name, err)
		}
	}
	return orphaned, nil
}

func (s SchemaBuilder) sortedOwnerCodecNames() []string {
	return slices.Sorted(maps.Keys(s.ownerCodecs))
}

// enumMarkers returns one EnumMarker per enum-marked type in the scanned
// package, sorted by type name so generation is deterministic. Each carries
// the type's first constant in the order ResolveEnum yields them (source
// declaration order); a marked type with no constants is rejected during
// scanning, so every entry has one.
func (s *SchemaBuilder) enumMarkers() []EnumMarker {
	markers := make([]EnumMarker, 0, len(s.Scan.Constants))
	for _, typeName := range slices.Sorted(maps.Keys(s.Scan.Constants)) {
		enumSet := s.Scan.Constants[typeName]
		marker := EnumMarker{
			TypeName: typeName,
			Constant: enumSet.Values[0].Name,
		}
		marker.Underlying, marker.IsString, marker.Members = enumCodecMembers(enumSet)
		markers = append(markers, marker)
	}
	return markers
}

// enumCodecMembers resolves the wire form of an enum-marked type: its
// underlying basic type, whether that type is a string, and one member per
// distinct wire value. It returns no members for anything it cannot encode
// (a non-basic or non-string/integer underlying type, or a constant whose
// evaluated value does not match its underlying kind), leaving the type
// without generated codecs rather than emitting code that will not compile.
func enumCodecMembers(enumSet *syntax.EnumSet) (underlying string, isString bool, members []EnumMember) {
	object := enumSet.TypeSpec.Pkg().Types.Scope().Lookup(enumSet.TypeSpec.Name())
	if object == nil {
		return "", false, nil
	}
	basic, ok := object.Type().Underlying().(*types.Basic)
	if !ok {
		return "", false, nil
	}
	switch {
	case basic.Info()&types.IsString != 0:
		isString = true
	case basic.Info()&types.IsInteger != 0:
	default:
		return "", false, nil
	}
	seen := make(map[string]bool, len(enumSet.Values))
	for _, value := range enumSet.Values {
		var wire string
		if isString {
			if value.Value.Kind() != constant.String {
				return "", false, nil
			}
			encoded, err := json.Marshal(constant.StringVal(value.Value))
			if err != nil {
				return "", false, nil
			}
			wire = string(encoded)
		} else {
			if value.Value.Kind() != constant.Int {
				return "", false, nil
			}
			wire = value.Value.ExactString()
		}
		if seen[wire] {
			continue
		}
		seen[wire] = true
		members = append(members, EnumMember{Constant: value.Name, Wire: wire})
	}
	return basic.Name(), isString, members
}

// RenderGoCode writes the generated Go file for the scanned package: it
// resolves the template data, renders it, and replaces whichever of the two
// generated file names this run owns.
func (s *SchemaBuilder) RenderGoCode() error {
	result, err := renderGoCode(s.templateData())
	if err != nil {
		return err
	}
	// The file is named for what it holds. The other name is removed when an
	// earlier run wrote it, so switching between schema and codec-only output
	// never leaves two generated files declaring the same methods.
	name, other := syntax.GeneratedCodecFile, syntax.GeneratedSchemaFile
	if s.GenerateSchemas {
		name, other = syntax.GeneratedSchemaFile, syntax.GeneratedCodecFile
	}
	otherPath := filepath.Join(s.Scan.Pkg.Dir, other)
	stale, err := ownedGeneratedFile(otherPath)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(s.Scan.Pkg.Dir, name), result, 0644); err != nil {
		return err
	}
	if stale {
		return os.Remove(otherPath)
	}
	return nil
}

// renderGoCode renders and formats the generated Go file from data alone. It
// is a pure function of its argument, consulting no scan, package graph or
// filesystem, so a test asserts on the code rendered from a value it built.
func renderGoCode(data schemaTemplateData) ([]byte, error) {
	rendered, err := RenderTemplate(schemasTemplate, data)
	if err != nil {
		return nil, err
	}
	return FormatCodeWithGoimports(rendered.Bytes())
}

// templateData resolves everything the generated file is rendered from. It
// is the only step of the rendering path that reads the scan: for the
// package clause, the import aliases, the schema entrypoints and the enum
// markers of the generated package.
func (s *SchemaBuilder) templateData() schemaTemplateData {
	importMap := s.imports()
	templateData := schemaTemplateData{
		PackageName:       s.Scan.Pkg.Name,
		BuildTag:          s.BuildTag,
		Subdir:            s.Subdir,
		Validate:          s.Validate,
		GenerateSchemas:   s.GenerateSchemas,
		DiscriminatorProp: s.DiscriminatorProp,
		Imports:           importMap.ImportStatements(),
		SchemaMethods:     schemaAccessors(s.SchemaMethods()),
		SchemaFreeFuncs:   schemaAccessors(s.SchemaFreeFuncs()),
		Rendered:          s.Rendered,
		RenderedTypes:     s.RenderedTypes,
		TypeProviders:     s.TypeProviders,
		EnumMarkers:       s.enumMarkers(),
	}
	generatedInterfaceHelpers := make(map[string]bool)

	for _, n := range s.sortedOwnerCodecNames() {
		owner := s.ownerCodecs[n]
		unionFields := slices.Clone(owner.UnionFields)
		for i := range unionFields {
			iface := unionFields[i].Union.Interface
			unionFields[i].InterfaceTypeNameWithPrefix = importMap.PrefixExpr(iface.Name, s.packageOf(iface.PackagePath))
		}
		enumFields := slices.Clone(owner.EnumFields)
		for i := range enumFields {
			enumPkg := s.packageOf(enumFields[i].EnumType.PackagePath)
			enumFields[i].EnumTypeNameWithPrefix = importMap.PrefixExpr(enumFields[i].EnumType.Name, enumPkg)
			enumFields[i].Entries = slices.Clone(enumFields[i].Entries)
			for j := range enumFields[i].Entries {
				enumFields[i].Entries[j].GoValueExpr = importMap.PrefixExpr(enumFields[i].Entries[j].ConstName, enumPkg)
			}
		}
		templateData.OwnerCodecs = append(templateData.OwnerCodecs, OwnerCodec{
			Name:        n,
			UnionFields: unionFields,
			EnumFields:  enumFields,
			Initial:     owner.Initial,
		})
		for _, prop := range unionFields {
			if generatedInterfaceHelpers[prop.helperIdentity()] {
				continue
			}
			generatedInterfaceHelpers[prop.helperIdentity()] = true
			iface := prop.Union.Interface
			var opts []InterfaceOptionInfo
			for _, variant := range prop.Union.Variants {
				opts = append(opts, InterfaceOptionInfo{
					TypeNameWithPrefix: importMap.PrefixExpr(variant.Implementation.Name, s.packageOf(variant.Implementation.PackagePath)),
					Discriminator:      variant.Tag,
					Pointer:            variant.Pointer,
				})
			}
			templateData.Interfaces = append(templateData.Interfaces, InterfaceInfo{
				TypeNameWithPrefix:    importMap.PrefixExpr(iface.Name, s.packageOf(iface.PackagePath)),
				TypeName:              iface.Name,
				MarshalerFunc:         prop.MarshalerFunc(),
				UnmarshalerFunc:       prop.UnmarshalerFunc(),
				DiscriminatorPropName: prop.DiscPropName,
				Options:               opts,
			})
		}
	}
	return templateData
}

// generatedGoHeader opens every Go file this generator writes.
const generatedGoHeader = "// Code generated by polytype. DO NOT EDIT."

// ownedGeneratedFile reports whether path holds Go code this generator wrote.
// A handwritten file, or another tool's generated file, of the same name is
// not polytype's to remove.
func ownedGeneratedFile(path string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly|parser.ParseComments)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect stale generated file: %w", err)
	}
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			if comment.Text == generatedGoHeader {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s SchemaBuilder) RenderSchemas(noChanges, force bool) (changedSchemas map[string]bool, orphaned []string, err error) {
	var targetDir = filepath.Join(s.Scan.Pkg.Dir, s.Subdir)
	changedSchemas = make(map[string]bool)
	expected := make(map[string]bool)

	var roots []syntax.SchemaMethod
	for _, root := range s.roots() {
		if s.hasSchema(root) {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		// No root has a schema: create nothing, but still prune what an
		// earlier run wrote.
		if _, statErr := os.Stat(targetDir); errors.Is(statErr, os.ErrNotExist) {
			return changedSchemas, nil, nil
		}
	} else if err = os.MkdirAll(targetDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("could not create subdir %s: %w", targetDir, err)
	}
	for _, root := range roots {
		artifactName := s.schemaArtifactName(root.Receiver)
		expected[artifactName] = true
		expected[artifactName+".sum"] = true
	}

	// Validate every removal before writing current schemas. This keeps a
	// modified orphan from causing an otherwise failed run to mutate outputs.
	orphaned, err = pruneOrphanedSchemaArtifacts(targetDir, expected, true)
	if err != nil {
		return nil, nil, err
	}

	for _, root := range roots {
		var changed bool
		if changed, err = s.writeSchema(root.Receiver, targetDir, noChanges); err != nil {
			return nil, nil, err
		}
		changedSchemas[root.Receiver.TypeName] = changed || force
	}
	if !noChanges && len(orphaned) > 0 {
		orphaned, err = pruneOrphanedSchemaArtifacts(targetDir, expected, false)
		if err != nil {
			return nil, nil, err
		}
	}
	return changedSchemas, orphaned, nil
}

func (s SchemaBuilder) resolveEmbeddedType(t syntax.TypeExpr, seen syntax.SeenTypes) (syntax.StructType, error) {
	switch expr := t.Excerpt.(type) {
	case *dst.Ident:
		if syntax.BasicTypes[expr.Name] {
			return syntax.NoStructType, fmt.Errorf("basic type %s is unsupported for embedding at %s", expr.Name, t.Position())
		}
		var pkgPath = expr.Path
		if pkgPath == "" {
			pkgPath = t.Pkg().PkgPath
		}
		if scan, ok := s.Scan.GetPackage(pkgPath); !ok {
			return syntax.NoStructType, fmt.Errorf("could not resolve package for type %s at %s", expr, t.Position())
		} else if ts, ok := scan.LocalNamedTypes[expr.Name]; !ok {
			return syntax.NoStructType, fmt.Errorf("could not resolve type %s at %s", expr, t.Position())
		} else {
			typeExpr := ts.Derive()
			switch _expr := typeExpr.Excerpt.(type) {
			case *dst.StructType:
				return syntax.NewStructType(_expr, *typeExpr.TypeSpec), nil
			case *dst.Ident:
				return s.resolveEmbeddedType(typeExpr, seen)
			case *dst.InterfaceType:
				return syntax.NoStructType, fmt.Errorf("embedded interface %s at %s is unsupported as a payload; use a named field of type %s instead", expr.Name, t.Position(), expr.Name)
			}
			return syntax.NoStructType, fmt.Errorf("embedded ident should be alias or struct type %s at %s", ts.Details(), ts.Position())
		}

	case *dst.StarExpr:
		return s.resolveEmbeddedType(t.Derive(expr.X), seen)
	case *dst.ParenExpr:
		return s.resolveEmbeddedType(t.Derive(expr.X), seen)
	default:
		return syntax.NoStructType, fmt.Errorf("unsupported embedded field %T at %s", expr, t.Position())
	}
}

func hasProviderForGoField(list []FieldProvider, goFieldNames []string) bool {
	for _, it := range list {
		if slices.Contains(goFieldNames, it.FieldName) {
			return true
		}
	}
	return false
}

type registeredInterfaceField struct {
	Interface    syntax.IfaceImplementations
	DiscPropName string
	Optional     bool
	Repeated     bool
}

func directInterfaceFieldType(expr dst.Expr) (ident *dst.Ident, repeated, ok bool) {
	switch value := expr.(type) {
	case *dst.Ident:
		return value, false, true
	case *dst.ArrayType:
		if value.Len != nil {
			return nil, false, false
		}
		ident, ok = value.Elt.(*dst.Ident)
		return ident, true, ok
	default:
		return nil, false, false
	}
}

func containsArrayType(expr dst.Expr) bool {
	found := false
	dst.Inspect(expr, func(node dst.Node) bool {
		if _, ok := node.(*dst.ArrayType); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}

func (s SchemaBuilder) resolveNamedType(ident *dst.Ident, localPkg *decorator.Package) (syntax.TypeSpec, bool) {
	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = localPkg.PkgPath
	}
	scan, ok := s.Scan.GetPackage(pkgPath)
	if !ok {
		return syntax.TypeSpec{}, false
	}
	typeSpec, ok := scan.LocalNamedTypes[ident.Name]
	return typeSpec, ok
}

func (s SchemaBuilder) registeredInterfaceInExpr(expr dst.Expr, localPkg *decorator.Package) (string, bool) {
	var interfaceName string
	dst.Inspect(expr, func(node dst.Node) bool {
		ident, ok := node.(*dst.Ident)
		if !ok {
			return true
		}
		if _, ok := s.findInterfaceImpl(ident, localPkg); ok {
			interfaceName = ident.Name
			return false
		}
		return true
	})
	return interfaceName, interfaceName != ""
}

func (s SchemaBuilder) resolveRegisteredInterfaceField(owner syntax.StructType, prop syntax.StructField) (*registeredInterfaceField, error) {
	fieldType := prop.Field.Type
	wrapper, inner, err := prop.Wrapper()
	if err != nil {
		return nil, err
	}
	if wrapper != syntax.WrapperNone {
		fieldType = inner
	}

	ident, repeated, direct := directInterfaceFieldType(fieldType)
	if direct {
		if iface, ok := s.findInterfaceImpl(ident, s.Scan.Pkg); ok {
			if repeated && wrapper != syntax.WrapperNone {
				return nil, fmt.Errorf("%s at %s", unsupportedRegisteredInterfaceContainer, prop.Position())
			}
			if wrapper == syntax.WrapperNullable {
				return nil, fmt.Errorf("%s does not support sealed interfaces at %s", wrapper, prop.Position())
			}
			return &registeredInterfaceField{
				Interface:    iface,
				DiscPropName: iface.Discriminator,
				Optional:     wrapper == syntax.WrapperOptional,
				Repeated:     repeated,
			}, nil
		}
		// A reachable interface field whose type is not a usable sealed
		// union is an error at the field. There is no explicit fallback.
		if diagnostic, found := s.interfaceDiagnostic(ident, s.Scan.Pkg); found {
			return nil, fmt.Errorf("field %s.%s at %s: %w", owner.Name(), fieldName(prop), prop.Position(), diagnostic)
		}
	}

	if interfaceName, found := s.registeredInterfaceInExpr(fieldType, s.Scan.Pkg); found {
		if containsArrayType(fieldType) {
			return nil, fmt.Errorf("%s for interface %s at %s", unsupportedRegisteredInterfaceContainer, interfaceName, prop.Position())
		}
		return nil, fmt.Errorf("found sealed interface type %s in an unsupported location at %s", interfaceName, prop.Position())
	}
	if ident, ok := fieldType.(*dst.Ident); ok {
		if typeSpec, found := s.resolveNamedType(ident, s.Scan.Pkg); found {
			if underlying, isArray := typeSpec.Type().Expr().(*dst.ArrayType); isArray {
				if interfaceName, containsInterface := s.registeredInterfaceInExpr(underlying, typeSpec.Pkg()); containsInterface {
					return nil, fmt.Errorf("%s for interface %s through named type %s at %s", unsupportedRegisteredInterfaceContainer, interfaceName, ident.Name, prop.Position())
				}
			}
		}
	}
	return nil, nil
}

// interfaceDiagnostic reports why the named interface ident is not a usable
// sealed union, when the scanner recorded such a reason.
func (s SchemaBuilder) interfaceDiagnostic(ident *dst.Ident, localPkg *decorator.Package) (error, bool) {
	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = localPkg.PkgPath
	}
	scan, ok := s.Scan.GetPackage(pkgPath)
	if !ok {
		return nil, false
	}
	diagnostic, ok := scan.InterfaceDiagnostics[ident.Name]
	return diagnostic, ok
}

func validateInterfaceDiscriminators(owner, fieldName string, field registeredInterfaceField) error {
	seen := make(map[string]syntax.TypeID, len(field.Interface.Impls))
	for _, impl := range field.Interface.Impls {
		value := field.Interface.DiscriminatorValue(impl)
		if previous, exists := seen[value]; exists {
			return fmt.Errorf(
				"field %s.%s: duplicate discriminator value %q for %s and %s; discriminator inflection must produce unique values",
				owner,
				fieldName,
				value,
				previous,
				impl,
			)
		}
		seen[value] = impl
	}
	return nil
}

func (s SchemaBuilder) findInterfaceImpl(ident *dst.Ident, localPkg *decorator.Package) (iface syntax.IfaceImplementations, ok bool) {
	var pkgPath = ident.Path
	if pkgPath == "" {
		pkgPath = localPkg.PkgPath
	}
	scan, ok := s.Scan.GetPackage(pkgPath)
	if !ok {
		// Package not found - this is normal for external packages like "time"
		return iface, false
	}
	iface, ok = scan.Interfaces[ident.Name]
	return iface, ok
}
