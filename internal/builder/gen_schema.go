package builder

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hash/fnv"

	"slices"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/tylergannon/polytype/internal/common"
	"github.com/tylergannon/polytype/internal/syntax"
)

//go:embed schemas.go.tmpl
var schemasTemplate string

const maxNestingDepth = 100 // This is not the JSON Schema nesting depth but recursion depth...
const defaultSubdir = "jsonschema"
const unsupportedRegisteredInterfaceContainer = "arrays/slices of registered interfaces are not yet supported"

func New(pkg *decorator.Package) (SchemaBuilder, error) {
	return NewForTypes(pkg, nil)
}

// NewForTypes constructs a builder for the selected registered schema roots.
// An empty selection preserves New's behavior and maps every registered root.
func NewForTypes(pkg *decorator.Package, typeNames []string) (SchemaBuilder, error) {
	data, err := syntax.LoadPackage(pkg)
	if err != nil {
		return SchemaBuilder{}, err
	}
	return newFromScan(data, typeNames, true)
}

func newFromScan(data syntax.ScanResult, typeNames []string, mapSchemas bool) (SchemaBuilder, error) {
	var err error
	var builder = SchemaBuilder{
		Scan:              data,
		schemas:           schemaMap{},
		customTypes:       map[string][]InterfaceProp{},
		Subdir:            defaultSubdir,
		BuildTag:          syntax.BuildTag,
		DiscriminatorProp: DefaultDiscriminatorPropName,
		TypeProvidersMap:  map[string][]FieldProvider{},
		EnumV1:            make(map[string]map[string]enumFieldConfig),
		enumFields:        make(map[string][]EnumFieldPlan),
		RenderedTypes:     []string{},
		Rendered:          map[string]bool{},
		RefTypes:          map[syntax.TypeID]bool{},
		RefDefs:           map[string]refDef{},
		GenerateSchemas:   true,
	}
	// First, collect providers so they're available during mapping
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
	if mapSchemas {
		// Map schema nodes and derive the generated Go codec plans only for
		// outputs that need them. Grammar-only backends never enter the JSON
		// Schema projection.
		for _, m := range data.SchemaMethods {
			if !selectedRoot(typeNames, m.Receiver.TypeName) {
				continue
			}
			if err = builder.mapType(m.Receiver, syntax.SeenTypes{}); err != nil {
				return builder, err
			}
		}
		for _, f := range data.SchemaFuncs {
			if !selectedRoot(typeNames, f.Receiver.TypeName) {
				continue
			}
			if err = builder.mapType(f.Receiver, syntax.SeenTypes{}); err != nil {
				return builder, err
			}
		}
		if err := builder.validateOwnerCodecMethods(); err != nil {
			return builder, err
		}
		if err := builder.validateEnumCodecMethods(); err != nil {
			return builder, err
		}
	}

	return builder, nil
}

func selectedRoot(typeNames []string, candidate string) bool {
	return len(typeNames) == 0 || slices.Contains(typeNames, candidate)
}

// OwnerCodec is the single composition point for all generated field codecs
// on a containing struct. Union fields are populated by #57; later adapters
// (such as field-specific enums) extend this owner instead of generating a
// competing MarshalJSON or UnmarshalJSON method.
type OwnerCodec struct {
	Name        string
	UnionFields []InterfaceProp
	EnumFields  []EnumFieldPlan
	Initial     string
}

type enumFieldConfig struct {
	UseStringer bool
}

type enumUnderlying int

const (
	enumUnderlyingInteger enumUnderlying = iota
	enumUnderlyingString
)

type EnumEntry struct {
	ConstName   string
	GoValueExpr string
	WireName    string
	NumberValue json.Number
}

// EnumFieldPlan is the resolved source of truth for one registered enum field.
// Schema rendering and generated field adapters consume the same entries.
type EnumFieldPlan struct {
	Field                  syntax.StructField
	GoName                 string
	JSONName               string
	Wrapper                syntax.WrapperKind
	EnumType               syntax.TypeSpec
	EnumTypeNameWithPrefix string
	Entries                []EnumEntry
	StringMode             bool
	Adapted                bool
	MarshalerFunc          string
	UnmarshalerFunc        string
}

func (e EnumFieldPlan) FieldNames() string { return e.GoName }
func (e EnumFieldPlan) StructTag() string {
	if e.Field.Field.Tag == nil {
		return ""
	}
	return e.Field.Field.Tag.Value
}
func (e EnumFieldPlan) Optional() bool { return e.Wrapper == syntax.WrapperOptional }
func (e EnumFieldPlan) Nullable() bool { return e.Wrapper == syntax.WrapperNullable }

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
	schemas           schemaMap
	customTypes       map[string][]InterfaceProp
	enumFields        map[string][]EnumFieldPlan
	Subdir            string
	Pretty            bool
	Validate          bool
	BuildTag          string
	DiscriminatorProp string
	// GenerateSchemas controls schema accessors and embedding in generated Go
	// code. Codec-only generation leaves it false and writes no schema assets.
	GenerateSchemas bool

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
	// Collected $defs entries, keyed by definition name, populated as
	// AsRef()'d types are rendered at their reference sites.
	RefDefs map[string]refDef
}

func (s SchemaBuilder) GeneratesJSONUnmarshalers() bool {
	return true
}

// HasGeneratedJSONCode reports whether the configured roots require enum or
// owner codecs. A GoJSON-only request for plain structs therefore performs no
// write instead of emitting an otherwise empty generated file.
func (s SchemaBuilder) HasGeneratedJSONCode() bool {
	return len(s.enumMarkers()) > 0 || len(s.sortedOwnerCodecNames()) > 0
}

// refDef pairs a $defs entry's schema with the TypeID it was generated from,
// so a second distinct type wanting the same definition name is caught as a
// collision rather than silently overwriting the first.
type refDef struct {
	TypeID syntax.TypeID
	Schema JSONSchema
}

// registerRefDef records (or reuses) a "$defs" entry for an AsRef()'d type
// and returns the RefNode that should be rendered in its place. A second,
// distinct type wanting the same bare definition name is a hard error.
func (s SchemaBuilder) registerRefDef(t syntax.TypeID, schema JSONSchema) (RefNode, error) {
	concrete := t.Concrete()
	name := concrete.TypeName
	ref := RefNode{Ref: "#/$defs/" + name}
	if existing, ok := s.RefDefs[name]; ok {
		if existing.TypeID != concrete {
			pos, _ := s.find(concrete)
			return RefNode{}, fmt.Errorf("AsRef definition name collision: %q is used by both %s and %s (registered at %s)", name, existing.TypeID, concrete, pos)
		}
		return ref, nil
	}
	s.RefDefs[name] = refDef{TypeID: concrete, Schema: schema}
	return ref, nil
}

// collectRefDefs walks a rendered schema tree and gathers every "$defs"
// entry reachable from it (transitively, since a $defs entry may itself
// reference another AsRef()'d type), keyed by bare definition name.
func (s SchemaBuilder) collectRefDefs(schema JSONSchema, defs map[string]JSONSchema) {
	switch node := schema.(type) {
	case ObjectNode:
		for _, prop := range node.Properties {
			s.collectRefDefs(prop.Schema, defs)
		}
	case ArrayNode:
		if node.Items != nil {
			s.collectRefDefs(node.Items, defs)
		}
	case UnionTypeNode:
		for _, opt := range node.Options {
			s.collectRefDefs(opt, defs)
		}
	case NullableObjectNode:
		s.collectRefDefs(node.Object, defs)
	case NullableUnionNode:
		s.collectRefDefs(node.Schema, defs)
	case RefNode:
		name := strings.TrimPrefix(node.Ref, "#/$defs/")
		if _, ok := defs[name]; ok {
			return
		}
		def, ok := s.RefDefs[name]
		if !ok {
			return
		}
		defs[name] = def.Schema
		s.collectRefDefs(def.Schema, defs)
	}
}

type schemaTemplateData struct {
	SchemaBuilder
	Imports     []string
	OwnerCodecs []OwnerCodec
	Interfaces  []InterfaceInfo
	// EnumMarkers lists every type in the generated package that declares
	// the func (T) enum() marker, sorted by type name. The template emits one
	// interface assertion per type, assigning the type's first typed constant,
	// so the marker is referenced from production code: that keeps its shape
	// checked at compile time and satisfies the staticcheck unused-method
	// check without any lint directives.
	EnumMarkers []EnumMarker
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

// HasNonRenderedTypes returns true if at least one schema method is for a non-rendered type.
func (s SchemaBuilder) HasNonRenderedTypes() bool {
	for _, m := range s.SchemaMethods() {
		if !s.Rendered[m.Receiver.TypeName] {
			return true
		}
	}
	return false
}

// discoverEnum auto-discovers an enum from const declarations in the package
func (s SchemaBuilder) discoverEnum(typeName string, scanRes syntax.ScanResult) (*syntax.EnumSet, error) {
	// Check if the type exists
	typeSpec, ok := scanRes.LocalNamedTypes[typeName]
	if !ok {
		return nil, nil
	}
	enumSet, err := syntax.ResolveEnum(typeSpec)
	if err != nil {
		return nil, err
	}
	if len(enumSet.Values) > 0 {
		return enumSet, nil
	}
	return nil, nil
}

func (s SchemaBuilder) imports() *ImportMap {
	importMap := NewImportMap(s.Scan.Pkg)
	// For each type that has any special interface handling,
	// need a
	for _, interfaceProps := range s.customTypes {
		for _, prop := range interfaceProps {
			importMap.AddPackage(prop.Interface.TypeSpec.Pkg())
			for _, implType := range prop.Interface.Impls {
				if scan, ok := s.Scan.GetPackage(implType.PkgPath); !ok {
					panic("internal error: no package found for " + implType.PkgPath)
				} else {
					importMap.AddPackage(scan.Pkg)
				}
			}
		}
	}
	for _, enumFields := range s.enumFields {
		for _, field := range enumFields {
			if field.Adapted {
				importMap.AddPackage(field.EnumType.Pkg())
			}
		}
	}
	return importMap
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

type schemaMap map[string]map[string]JSONSchema

func (m schemaMap) Set(pkgPath, typeName string, schema JSONSchema) {
	if m[pkgPath] == nil {
		m[pkgPath] = make(map[string]JSONSchema)
	}
	m[pkgPath][typeName] = schema
}
func (m schemaMap) Get(pkgPath, typeName string) (schema JSONSchema, ok bool) {
	var _m map[string]JSONSchema
	if _m, ok = m[pkgPath]; !ok {
		return
	}
	schema, ok = _m[typeName]
	return
}

func (s SchemaBuilder) GetSchema(t syntax.TypeID) (schema JSONSchema, ok bool) {
	return s.schemas.Get(t.PkgPath, t.TypeName)
}

func (s SchemaBuilder) AddSchema(t syntax.TypeID, schema JSONSchema) {
	ty := t.Concrete()
	s.schemas.Set(ty.PkgPath, ty.TypeName, schema)
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

func (s SchemaBuilder) mapInterface(iface syntax.IfaceImplementations, seen syntax.SeenTypes) error {
	if seen.Seen(iface.TypeSpec.ID()) {
		return fmt.Errorf("circular dependency found for type %s, defined at %s", iface.TypeSpec.ID(), iface.TypeSpec.Position())
	}
	seen = seen.See(iface.TypeSpec.ID())
	if err := s.checkSeen(seen); err != nil {
		return err
	}

	node := UnionTypeNode{
		TypeID_:               iface.TypeSpec.ID(),
		DiscriminatorPropName: iface.Discriminator,
	}
	discriminator := iface.Discriminator
	if discriminator == "" {
		discriminator = s.DiscriminatorProp
	}
	if discriminator == "" {
		discriminator = DefaultDiscriminatorPropName
	}
	for _, opt := range iface.Impls {
		if err := s.mapType(opt, seen); err != nil {
			return err
		}
		optSchema, ok := s.GetSchema(opt)
		if !ok {
			return fmt.Errorf("type %s is not a known schema", opt)
		}
		obj, ok := optSchema.(ObjectNode)
		if !ok {
			pos, err := s.find(opt)
			if err != nil {
				return err
			}
			return fmt.Errorf("expected %s to be an object-type schema at %s", opt.TypeName, pos)
		}
		for _, property := range obj.Properties {
			if property.Name == discriminator {
				pos, _ := s.find(opt)
				return fmt.Errorf("variant %s of sealed interface %s has a payload property %q that collides with the discriminator property at %s", opt.TypeName, iface.TypeSpec.Name(), discriminator, pos)
			}
		}
		obj.Discriminator = iface.DiscriminatorValue(opt)
		node.Options = append(node.Options, obj)
	}
	s.AddSchema(iface.TypeSpec.ID(), node)
	return nil
}

func (s SchemaBuilder) mapEnumType(enum *syntax.EnumSet, seen syntax.SeenTypes) error {
	seen = seen.See(enum.TypeSpec.ID())
	if err := s.checkSeen(seen); err != nil {
		return err
	}

	schema, err := renderEnum(enum, false, enum.TypeSpec.Comments(), true, enum.TypeSpec.ID())
	if err != nil {
		return err
	}
	s.AddSchema(enum.TypeSpec.ID(), schema)
	return nil
}

func renderEnum(enum *syntax.EnumSet, names bool, description string, withDescriptions bool, typeID syntax.TypeID) (JSONSchema, error) {
	if len(enum.Values) == 0 {
		return nil, fmt.Errorf("enum %s at %s has no constants of its exact named type", enum.TypeSpec.Name(), enum.TypeSpec.Position())
	}
	object := enum.TypeSpec.Pkg().Types.Scope().Lookup(enum.TypeSpec.Name())
	basic, ok := object.Type().Underlying().(*types.Basic)
	if !ok {
		return nil, fmt.Errorf("enum %s at %s has unsupported underlying type %s", enum.TypeSpec.Name(), enum.TypeSpec.Position(), object.Type().Underlying())
	}
	if basic.Info()&types.IsString != 0 {
		values := make([]string, 0, len(enum.Values))
		for _, member := range enum.Values {
			if member.Value.Kind() != constant.String {
				return nil, fmt.Errorf("enum %s constant %s at %s is not an exact string", enum.TypeSpec.Name(), member.Name, member.Source)
			}
			values = append(values, constant.StringVal(member.Value))
		}
		return PropertyNode[string]{Typ: "string", Enum: values, Desc: enumDescription(description, enum.Values, values, withDescriptions), TypeID_: typeID}, nil
	}
	if basic.Info()&types.IsInteger == 0 {
		return nil, fmt.Errorf("enum %s at %s must have a string or integer underlying type", enum.TypeSpec.Name(), enum.TypeSpec.Position())
	}
	if names {
		values := make([]string, 0, len(enum.Values))
		seen := make(map[string]string, len(enum.Values))
		for _, member := range enum.Values {
			exact := member.Value.ExactString()
			if previous, duplicate := seen[exact]; duplicate {
				return nil, fmt.Errorf("enum %s has ambiguous string-mode constants %s and %s with value %s", enum.TypeSpec.Name(), previous, member.Name, exact)
			}
			seen[exact] = member.Name
			values = append(values, member.Name)
		}
		return PropertyNode[string]{Typ: "string", Enum: values, Desc: enumDescription(description, enum.Values, values, withDescriptions), TypeID_: typeID}, nil
	}
	values := make([]json.Number, 0, len(enum.Values))
	labels := make([]string, 0, len(enum.Values))
	for _, member := range enum.Values {
		if member.Value.Kind() != constant.Int {
			return nil, fmt.Errorf("enum %s constant %s at %s is not an exact integer", enum.TypeSpec.Name(), member.Name, member.Source)
		}
		exact := member.Value.ExactString()
		values = append(values, json.Number(exact))
		labels = append(labels, exact)
	}
	return PropertyNode[json.Number]{Typ: "integer", Enum: values, Desc: enumDescription(description, enum.Values, labels, withDescriptions), TypeID_: typeID}, nil
}

func enumDescription(base string, members []syntax.EnumValue, wireValues []string, enabled bool) string {
	if !enabled {
		return ""
	}
	var comments strings.Builder
	for i, member := range members {
		if member.Description == "" {
			continue
		}
		if comments.Len() > 0 {
			comments.WriteString("\n\n")
		}
		comments.WriteString(wireValues[i])
		comments.WriteString(": \n")
		comments.WriteString(member.Description)
	}
	if base != "" && comments.Len() > 0 {
		return base + "\n\n" + comments.String()
	}
	if base != "" {
		return base
	}
	return comments.String()
}

// mapType
func (s SchemaBuilder) mapType(t syntax.TypeID, seen syntax.SeenTypes) error {
	scanResult, err := s.loadScanResult(t)
	if err != nil {
		return err
	}
	if iface, ok := scanResult.Interfaces[t.TypeName]; ok {
		if err = s.mapInterface(iface, seen); err != nil {
			return err
		}
	} else if enum, ok := scanResult.Constants[t.TypeName]; ok {
		if err = s.mapEnumType(enum, seen); err != nil {
			return err
		}
	} else if err = s.mapNamedType(t, seen); err != nil {
		return err
	}

	return nil
}

func (s SchemaBuilder) checkSeen(seen syntax.SeenTypes) error {
	if len(seen) > maxNestingDepth {
		pos, _ := s.find(seen[0])
		return fmt.Errorf("max nesting depth exceeded at %s", pos)
	}
	return nil
}

func (s SchemaBuilder) mapNamedType(t syntax.TypeID, seen syntax.SeenTypes) error {
	scanResult, err := s.loadScanResult(t)
	if err != nil {
		return err
	}
	typeSpec, ok := scanResult.LocalNamedTypes[t.TypeName]
	if !ok {
		return fmt.Errorf("mapNamedType: type %s not found", t.TypeName)
	}
	if seen.Seen(t) {
		return fmt.Errorf("circular dependency found for type %s at %s", t.TypeName, typeSpec.Position())
	}
	if structType, ok := typeSpec.Type().Expr().(*dst.StructType); ok {
		enumFields, enumErr := s.resolveLocalEnumFields(syntax.NewStructType(structType, typeSpec))
		if enumErr != nil {
			return enumErr
		}
		if len(enumFields) > 0 {
			s.enumFields[t.TypeName] = enumFields
		}
		if props, err := s.resolveLocalInterfaceProps(syntax.NewStructType(structType, typeSpec), nil, nil); err != nil {
			return err
		} else if err := validateOwnerCodecInterfaceFields(t.TypeName, props); err != nil {
			return err
		} else if len(props) > 0 {
			s.customTypes[t.TypeName] = props
		}
	}
	if schema, err := s.renderSchema(typeSpec.Derive(), typeSpec.Comments(), seen); err != nil {
		return err
	} else {
		s.AddSchema(t, schema)
	}
	return nil
}

func (s SchemaBuilder) resolveLocalEnumFields(owner syntax.StructType) ([]EnumFieldPlan, error) {
	configs := s.EnumV1[owner.Name()]
	if len(configs) == 0 {
		return nil, nil
	}
	fields := make(map[string]syntax.StructField)
	for _, field := range owner.Fields() {
		for _, name := range field.Field.Names {
			fields[name.Name] = field
		}
	}
	fieldNames := make([]string, 0, len(configs))
	for fieldName := range configs {
		fieldNames = append(fieldNames, fieldName)
	}
	slices.Sort(fieldNames)
	plans := make([]EnumFieldPlan, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		field, ok := fields[fieldName]
		if !ok {
			return nil, fmt.Errorf("field %s.%s: registered enum field was not found", owner.Name(), fieldName)
		}
		if len(field.Field.Names) != 1 || field.Skip() {
			return nil, fmt.Errorf("field %s.%s: registered enum must be a single JSON field", owner.Name(), fieldName)
		}
		plan, err := s.resolveEnumFieldPlan(owner.Name(), fieldName, field, configs[fieldName])
		if err != nil {
			return nil, err
		}
		if plan != nil {
			plans = append(plans, *plan)
		}
	}
	return plans, nil
}

func (s SchemaBuilder) resolveEnumFieldPlan(owner, fieldName string, field syntax.StructField, config enumFieldConfig) (*EnumFieldPlan, error) {
	if field.HasJSONOption("string") {
		return nil, fmt.Errorf("field %s.%s: registered enum fields do not support json:\",string\" at %s", owner, fieldName, field.Position())
	}
	wrapper, inner, err := field.Wrapper()
	if err != nil {
		return nil, err
	}
	fieldType := field.Type()
	if wrapper != syntax.WrapperNone {
		fieldType = inner
	}
	ident, direct := fieldType.(*dst.Ident)
	if !direct {
		return nil, fmt.Errorf("field %s.%s: .StringerEnum supports only a direct named enum, Optional[E], or Nullable[E] at %s", owner, fieldName, field.Position())
	}
	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = s.Scan.Pkg.PkgPath
	}
	scanResult, ok := s.Scan.GetPackage(pkgPath)
	if !ok {
		return nil, fmt.Errorf("field %s.%s: could not resolve enum package %s", owner, fieldName, pkgPath)
	}
	enumSet, ok := scanResult.Constants[ident.Name]
	if !ok {
		enumSet, err = s.discoverEnum(ident.Name, scanResult)
		if err != nil {
			return nil, fmt.Errorf("field %s.%s: resolving enum type %s: %w", owner, fieldName, ident.Name, err)
		}
	}
	if enumSet == nil {
		return nil, fmt.Errorf("field %s.%s: no constants declared for enum type %s", owner, fieldName, ident.Name)
	}
	object := scanResult.Pkg.Types.Scope().Lookup(ident.Name)
	if object == nil {
		return nil, fmt.Errorf("field %s.%s: could not resolve enum type %s", owner, fieldName, ident.Name)
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("field %s.%s: enum type %s is not a defined type", owner, fieldName, ident.Name)
	}
	basic, ok := named.Underlying().(*types.Basic)
	if !ok {
		return nil, fmt.Errorf("field %s.%s: enum type %s must have an integer or string underlying type", owner, fieldName, ident.Name)
	}
	underlying := enumUnderlyingInteger
	switch {
	case basic.Info()&types.IsInteger != 0:
	case basic.Info()&types.IsString != 0:
		underlying = enumUnderlyingString
	default:
		return nil, fmt.Errorf("field %s.%s: enum type %s must have an integer or string underlying type", owner, fieldName, ident.Name)
	}

	entries, err := enumEntries(enumSet, underlying, config.UseStringer, scanResult.Pkg.PkgPath != s.Scan.Pkg.PkgPath)
	if err != nil {
		return nil, fmt.Errorf("field %s.%s: %w", owner, fieldName, err)
	}
	jsonNames := field.PropNames()
	if len(jsonNames) != 1 {
		return nil, fmt.Errorf("field %s.%s: registered enum must resolve to one JSON property", owner, fieldName)
	}
	adapted := config.UseStringer && underlying == enumUnderlyingInteger
	if adapted {
		methods, err := syntax.FindProductionJSONMethods(scanResult.Pkg.Dir, []string{ident.Name})
		if err != nil {
			return nil, fmt.Errorf("field %s.%s: discovering enum JSON methods: %w", owner, fieldName, err)
		}
		if len(methods) > 0 {
			return nil, fmt.Errorf("field %s.%s: cannot adapt string-mode enum %s because production %s is declared at %s", owner, fieldName, ident.Name, methods[0].Name, methods[0].Position)
		}
	}
	return &EnumFieldPlan{
		Field:           field,
		GoName:          fieldName,
		JSONName:        jsonNames[0],
		Wrapper:         wrapper,
		EnumType:        enumSet.TypeSpec,
		Entries:         entries,
		StringMode:      config.UseStringer || underlying == enumUnderlyingString,
		Adapted:         adapted,
		MarshalerFunc:   "__jsonMarshalEnum__" + owner + "__" + fieldName,
		UnmarshalerFunc: "__jsonUnmarshalEnum__" + owner + "__" + fieldName,
	}, nil
}

func enumEntries(enumSet *syntax.EnumSet, underlying enumUnderlying, stringMode, remote bool) ([]EnumEntry, error) {
	seenValues := map[string]string{}
	var entries []EnumEntry
	for _, member := range enumSet.Values {
		if remote && !token.IsExported(member.Name) && stringMode && underlying == enumUnderlyingInteger {
			return nil, fmt.Errorf("string-mode enum constant %s is not exported", member.Name)
		}
		exact := member.Value.ExactString()
		if previous, exists := seenValues[exact]; exists && previous != member.Name {
			if stringMode && underlying == enumUnderlyingInteger {
				return nil, fmt.Errorf("enum constants %s and %s have duplicate underlying value %s", previous, member.Name, exact)
			}
			continue
		}
		seenValues[exact] = member.Name
		entry := EnumEntry{ConstName: member.Name}
		switch underlying {
		case enumUnderlyingString:
			if member.Value.Kind() != constant.String {
				return nil, fmt.Errorf("enum constant %s at %s is not an exact string", member.Name, member.Source)
			}
			entry.WireName = constant.StringVal(member.Value)
		case enumUnderlyingInteger:
			if member.Value.Kind() != constant.Int {
				return nil, fmt.Errorf("enum constant %s at %s is not an exact integer", member.Name, member.Source)
			}
			entry.WireName = member.Name
			if !stringMode {
				entry.NumberValue = json.Number(exact)
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("enum %s has no declared constants", enumSet.TypeSpec.Name())
	}
	return entries, nil
}

func (s SchemaBuilder) renderSchema(t syntax.TypeExpr, description string, seen syntax.SeenTypes) (JSONSchema, error) {
	switch node := t.Excerpt.(type) {
	case *dst.Ident:
		switch node.Name {
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
			return PropertyNode[int]{Desc: description, Typ: "integer", TypeID_: t.ID()}, nil
		case "string":
			return PropertyNode[string]{Desc: description, Typ: "string", TypeID_: t.ID()}, nil
		case "bool":
			return PropertyNode[bool]{Desc: description, Typ: "boolean", TypeID_: t.ID()}, nil
		case "float32", "float64":
			return PropertyNode[float64]{Desc: description, Typ: "number", TypeID_: t.ID()}, nil
		default:
			// Special handling for well-known external types
			if syntax.IsTimeType(node.Path, node.Name) {
				// time.Time should be represented as a string with RFC3339 format
				// We add description to guide LLMs rather than using format field
				timeDesc := "RFC3339 formatted date-time string (e.g., \"2006-01-02T15:04:05Z07:00\")"
				if description != "" {
					timeDesc = description + ". Must be an " + timeDesc
				}
				return PropertyNode[string]{
					Desc:    timeDesc,
					Typ:     "string",
					TypeID_: t.ID(),
				}, nil
			}

			// Means it is another named type.
			// Find it.
			newType := syntax.TypeID{TypeName: node.Name, PkgPath: node.Path}
			if newType.PkgPath == "" {
				newType.PkgPath = t.Pkg().PkgPath
			}

			// Check if this is an external package that we haven't scanned
			if newType.PkgPath != "" {
				if _, ok := s.Scan.GetPackage(newType.PkgPath); !ok {
					// External package not scanned - return an empty schema (allows any valid JSON)
					// We return an empty ObjectNode which will be rendered as {}
					return ObjectNode{
						Desc:    description,
						TypeID_: t.ID(),
					}, nil
				}
			}

			if err := s.mapType(newType, seen.See(t.ID())); err != nil {
				return nil, err
			}
			schema, ok := s.GetSchema(newType)
			if !ok {
				panic("mapType apparently didn't map the type! " + newType.String())
			}
			if s.RefTypes[newType.Concrete()] {
				return s.registerRefDef(newType, schema)
			}
			if description == "" {
				return schema, nil
			}
			if _schemaNode, ok := schema.(schemaNode); !ok {
				return schema, nil
			} else {
				return _schemaNode.setDescription(description), nil
			}
		}
	case *dst.StarExpr:
		return s.renderSchema(t.Derive(node.X), description, seen)
	case *dst.ParenExpr:
		return s.renderSchema(t.Derive(node.X), description, seen)
	case *dst.ArrayType:
		var (
			err    error
			schema = ArrayNode{Desc: description, TypeID_: t.ID()}
		)
		if schema.Items, err = s.renderSchema(t.Derive(node.Elt), "", seen); err != nil {
			return nil, err
		}
		if _, isUnion := schema.Items.(UnionTypeNode); isUnion {
			return nil, fmt.Errorf("%s at %s", unsupportedRegisteredInterfaceContainer, t.Position())
		}
		return schema, nil
	case *dst.MapType, *dst.ChanType:
		return nil, fmt.Errorf("mapType/chanType not allowed %s at %s", t.Name(), t.Position())
	case *dst.StructType:
		return s.renderStructSchema(syntax.NewStructType(node, *t.TypeSpec), description, seen)
	case *dst.InterfaceType:
		return nil, fmt.Errorf("interface types are not supported. Found on %s at %s", t.ID(), t.Position())
	default:
		return nil, fmt.Errorf("unhandled schema node %s at %s", t.ToExpr().Details(), t.ToExpr().Position())
	}
}

func (s SchemaBuilder) renderStructSchema(t syntax.StructType, description string, seen syntax.SeenTypes) (node ObjectNode, err error) {
	node = ObjectNode{
		Desc:          description,
		Discriminator: t.Name(),
		TypeID_:       t.ID(),
	}
	node.Properties, err = s.renderStructProps(t, nil, seen)
	return node, err
}

func (s SchemaBuilder) writeSchema(t syntax.TypeID, targetDir string, noChanges bool) (wroteNew bool, err error) {
	var (
		ok       bool
		filePath string
		sumPath  string
		tmpFile  *os.File
	)

	// Decide target file extension based on whether schema is templated
	if _, templated := s.TypeProvidersMap[t.TypeName]; templated {
		filePath = filepath.Join(targetDir, fmt.Sprintf("%s.json.tmpl", t.TypeName))
	} else {
		filePath = filepath.Join(targetDir, fmt.Sprintf("%s.json", t.TypeName))
	}
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

	rootSchema, ok := s.GetSchema(t)
	if !ok {
		return false, fmt.Errorf("unknown type %s", t)
	}
	var schema json.Marshaler = rootSchema
	defs := map[string]JSONSchema{}
	s.collectRefDefs(rootSchema, defs)
	if len(defs) > 0 {
		schema = RootSchema{Root: rootSchema, Defs: defs}
	}

	hash := fnv.New64a()
	writer := io.MultiWriter(tmpFile, hash)
	_, templated := s.TypeProvidersMap[t.TypeName]
	// Templates cannot use the standard pretty encoder because their holes are
	// not valid JSON. Preserve the existing raw output when pretty is requested.
	if templated && s.Pretty {
		var b []byte
		if b, err = schema.MarshalJSON(); err != nil {
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
		if err = encoder.Encode(schema); err != nil {
			return false, fmt.Errorf("could not encode schema: %w", err)
		}
	} else {
		var b []byte
		if b, err = marshalSchemaHardlines(schema); err != nil {
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

func (s SchemaBuilder) sortedOwnerCodecNames() []string {
	owners := map[string]bool{}
	for name := range s.customTypes {
		owners[name] = true
	}
	for name, fields := range s.enumFields {
		if slices.ContainsFunc(fields, func(field EnumFieldPlan) bool { return field.Adapted }) {
			owners[name] = true
		}
	}
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
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

func (s *SchemaBuilder) RenderGoCode() (err error) {
	importMap := s.imports()
	templateData := schemaTemplateData{
		SchemaBuilder: *s,
		Imports:       importMap.ImportStatements(),
		EnumMarkers:   s.enumMarkers(),
	}
	generatedInterfaceHelpers := make(map[string]bool)

	for _, n := range s.sortedOwnerCodecNames() {
		itsProps := slices.Clone(s.customTypes[n])
		for i := range itsProps {
			ifacePkg := itsProps[i].Interface.TypeSpec.Pkg()
			itsProps[i].InterfaceTypeNameWithPrefix = importMap.PrefixExpr(itsProps[i].Interface.TypeSpec.Name(), ifacePkg)
		}
		enumFields := slices.Clone(s.enumFields[n])
		enumFields = slices.DeleteFunc(enumFields, func(field EnumFieldPlan) bool { return !field.Adapted })
		for i := range enumFields {
			enumPkg := enumFields[i].EnumType.Pkg()
			enumFields[i].EnumTypeNameWithPrefix = importMap.PrefixExpr(enumFields[i].EnumType.Name(), enumPkg)
			for j := range enumFields[i].Entries {
				enumFields[i].Entries[j].GoValueExpr = importMap.PrefixExpr(enumFields[i].Entries[j].ConstName, enumPkg)
			}
		}
		templateData.OwnerCodecs = append(templateData.OwnerCodecs, OwnerCodec{
			Name:        n,
			UnionFields: itsProps,
			EnumFields:  enumFields,
			Initial:     strings.ToLower(n[0:1]),
		})
		for _, ifaceProp := range itsProps {
			if generatedInterfaceHelpers[ifaceProp.helperIdentity()] {
				continue
			}
			generatedInterfaceHelpers[ifaceProp.helperIdentity()] = true
			ifacePkg := ifaceProp.Interface.TypeSpec.Pkg()
			var opts []InterfaceOptionInfo
			for _, option := range ifaceProp.Interface.Impls {
				pkg, ok := s.Scan.GetPackage(option.PkgPath)
				if !ok {
					panic("could not find package at RenderGoCode: " + option.PkgPath)
				}
				opts = append(opts, InterfaceOptionInfo{
					TypeNameWithPrefix: importMap.PrefixExpr(option.TypeName, pkg.Pkg),
					Discriminator:      ifaceProp.Interface.DiscriminatorValue(option),
					Pointer:            option.Indirection == syntax.Pointer,
				})
			}
			// Determine discriminator property name for this field-specific unmarshaler (only if overridden)
			discProp := ifaceProp.DiscPropName
			templateData.Interfaces = append(templateData.Interfaces, InterfaceInfo{

				TypeNameWithPrefix:    importMap.PrefixExpr(ifaceProp.Interface.TypeSpec.Name(), ifacePkg),
				TypeName:              ifaceProp.Interface.TypeSpec.Name(),
				MarshalerFunc:         ifaceProp.MarshalerFunc(),
				UnmarshalerFunc:       ifaceProp.UnmarshalerFunc(),
				DiscriminatorPropName: discProp,
				Options:               opts,
			})
		}
	}
	data, err := RenderTemplate(schemasTemplate, templateData)
	if err != nil {
		return err
	}
	result, err := FormatCodeWithGoimports(data.Bytes())
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(s.Scan.Pkg.Dir, "jsonschema_gen.go"), result, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (s SchemaBuilder) RenderSchemas(noChanges, force bool) (changedSchemas map[string]bool, err error) {
	var targetDir = filepath.Join(s.Scan.Pkg.Dir, s.Subdir)
	changedSchemas = make(map[string]bool)

	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create subdir %s: %w", targetDir, err)
	}
	for _, method := range s.Scan.SchemaMethods {
		var changed bool
		if changed, err = s.writeSchema(method.Receiver, targetDir, noChanges); err != nil {
			return nil, err
		}
		changedSchemas[method.Receiver.TypeName] = changed || force
	}
	for _, fn := range s.Scan.SchemaFuncs {
		var changed bool
		if changed, err = s.writeSchema(fn.Receiver, targetDir, noChanges); err != nil {
			return nil, err
		}
		changedSchemas[fn.Receiver.TypeName] = changed || force
	}
	return changedSchemas, nil
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

func (s SchemaBuilder) renderStructProps(t syntax.StructType, seenProps syntax.SeenProps, seen syntax.SeenTypes) (props ObjectPropSet, err error) {
	var myProps = slices.Clone(seenProps)
	for _, prop := range t.Fields() {
		if prop.Skip() {
			continue
		}
		for _, name := range prop.PropNames() {
			myProps = myProps.See(name)
		}
	}
	for _, prop := range t.Fields() {
		var tempProps ObjectPropSet
		if prop.Skip() {
			continue
		}
		if prop.Embedded() {
			wrapper, _, wrapperErr := prop.Wrapper()
			if wrapperErr != nil {
				return nil, wrapperErr
			}
			if contractErr := validateStaticFieldWireContract(t, prop, wrapper); contractErr != nil {
				return nil, contractErr
			}
			var embeddedType syntax.StructType
			if embeddedType, err = s.resolveEmbeddedType(prop.TypeExpr, seen); err != nil {
				return nil, fmt.Errorf("resolving embedded type: %w", err)
			} else if tempProps, err = s.renderStructProps(embeddedType, myProps, seen); err != nil {
				return nil, fmt.Errorf("rendering embedded type: %w", err)
			}
		} else if tempProps, err = s.renderStructField(t, prop, seen); err != nil {
			return nil, fmt.Errorf("rendering struct field: %w", err)
		}
		props = append(props, tempProps...)
	}
	return props, nil
}

func hasProviderForGoField(list []FieldProvider, goFieldNames []string) bool {
	for _, it := range list {
		if slices.Contains(goFieldNames, it.FieldName) {
			return true
		}
	}
	return false
}

func (s SchemaBuilder) renderStructField(owner syntax.StructType, f syntax.StructField, seen syntax.SeenTypes) (props []ObjectProp, err error) {
	var (
		schema        JSONSchema
		name          string
		specialSource string
	)
	wrapper, inner, err := f.Wrapper()
	if err != nil {
		return nil, err
	}
	if err := validateStaticFieldWireContract(owner, f, wrapper); err != nil {
		return nil, err
	}
	if wrapper == syntax.WrapperOptional && !f.HasJSONOption("omitzero") {
		return nil, fmt.Errorf("%s field %s requires json:\",omitzero\" at %s", wrapper, strings.Join(f.PropNames(), ","), f.Position())
	}
	renderType := f.Type()
	if wrapper != syntax.WrapperNone {
		renderType = inner
	}
	interfaceField, err := s.resolveRegisteredInterfaceField(owner, f)
	if err != nil {
		return nil, err
	}
	// Prefer centralized tag parsing
	if f.Field.Tag != nil && f.Field.Tag.Value != "" {
		if tag := common.ParseJSONSchemaTag(f.Field.Tag.Value); tag.HasRef {
			schema = RefNode{Ref: tag.Ref}
			specialSource = "explicit refs"
		}
	}
	if schema == nil {
		if plan, ok := s.resolvedEnumField(owner.Name(), f); ok {
			schema = plan.schema(f.ID())
			specialSource = "enums"
		}
		// Registered interfaces, including the one supported container shape:
		// a direct one-dimensional slice of the registered interface.
		if interfaceField != nil {
			union, unionErr := s.renderRegisteredInterfaceUnion(*interfaceField, f, seen)
			if unionErr != nil {
				return nil, unionErr
			}
			if interfaceField.Repeated {
				schema = ArrayNode{Desc: f.Comments(), Items: union, TypeID_: f.ID()}
			} else {
				schema = union
			}
			specialSource = "registered interfaces"
		}
		// Providers
		if schema == nil {
			if providers, ok := s.TypeProvidersMap[f.Name()]; ok {
				var goNames []string
				for _, ident := range f.Field.Names {
					goNames = append(goNames, ident.Name)
				}
				if hasProviderForGoField(providers, goNames) {
					jsonNames := f.PropNames()
					if len(jsonNames) > 0 {
						schema = TemplateHoleNode{Name: jsonNames[0]}
						specialSource = "providers"
					}
				}
			}
		}
		// Fallback
		if schema == nil {
			if schema, err = s.renderSchema(f.Derive(renderType), f.Comments(), seen); err != nil {
				return nil, fmt.Errorf("rendering schema: %w", err)
			}
		}
	}
	if wrapper == syntax.WrapperNullable {
		if _, isArrayOrSlice := renderType.(*dst.ArrayType); isArrayOrSlice {
			return nil, fmt.Errorf("%s does not support arrays/slices at %s", wrapper, f.Position())
		}
		if specialSource != "" && specialSource != "enums" {
			return nil, fmt.Errorf("%s does not support %s at %s", wrapper, specialSource, f.Position())
		}
		if schema, err = nullableSchema(schema); err != nil {
			return nil, fmt.Errorf("%s field %s at %s: %w", wrapper, strings.Join(f.PropNames(), ","), f.Position(), err)
		}
	}
	for _, name = range f.PropNames() {
		props = append(props, ObjectProp{
			Name:     name,
			Schema:   schema,
			Optional: !f.Required(),
		})
	}
	return props, nil
}

func (s SchemaBuilder) resolvedEnumField(owner string, field syntax.StructField) (EnumFieldPlan, bool) {
	for _, plan := range s.enumFields[owner] {
		for _, name := range field.Field.Names {
			if plan.GoName == name.Name {
				return plan, true
			}
		}
	}
	return EnumFieldPlan{}, false
}

func (e EnumFieldPlan) schema(typeID syntax.TypeID) JSONSchema {
	if e.StringMode {
		values := make([]string, 0, len(e.Entries))
		for _, entry := range e.Entries {
			values = append(values, entry.WireName)
		}
		return PropertyNode[string]{Typ: "string", Enum: values, TypeID_: typeID}
	}
	values := make([]json.Number, 0, len(e.Entries))
	for _, entry := range e.Entries {
		values = append(values, entry.NumberValue)
	}
	return PropertyNode[json.Number]{Typ: "integer", Enum: values, TypeID_: typeID}
}

func nullableSchema(schema JSONSchema) (JSONSchema, error) {
	switch value := schema.(type) {
	case PropertyNode[int]:
		return nullableProperty(value)
	case PropertyNode[json.Number]:
		return nullableProperty(value)
	case PropertyNode[string]:
		return nullableProperty(value)
	case PropertyNode[bool]:
		return nullableProperty(value)
	case PropertyNode[float64]:
		return nullableProperty(value)
	case ObjectNode:
		return NullableObjectNode{Object: value}, nil
	case RefNode:
		return NullableUnionNode{Schema: value}, nil
	default:
		return nil, fmt.Errorf("inner schema shape %T is unsupported; supported nullable values are scalars, enums, structs, pointers to structs, and AsRef structs", schema)
	}
}

func nullableProperty[T ~int | ~string | ~bool | float32 | float64](value PropertyNode[T]) (JSONSchema, error) {
	if value.Const != nil {
		return nil, errors.New("consts are unsupported; supported nullable values are scalars, enums, structs, pointers to structs, and AsRef structs")
	}
	if len(value.Enum) > 0 {
		return NullableUnionNode{Schema: value}, nil
	}
	value.Nullable = true
	return value, nil
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

func (s SchemaBuilder) renderRegisteredInterfaceUnion(field registeredInterfaceField, prop syntax.StructField, seen syntax.SeenTypes) (UnionTypeNode, error) {
	if err := s.mapType(field.Interface.TypeSpec.ID(), seen); err != nil {
		return UnionTypeNode{}, fmt.Errorf("rendering interface: %w", err)
	}
	schema, ok := s.GetSchema(field.Interface.TypeSpec.ID())
	if !ok {
		return UnionTypeNode{}, fmt.Errorf("interface %s is not a known schema", field.Interface.TypeSpec.Name())
	}
	union, ok := schema.(UnionTypeNode)
	if !ok {
		return UnionTypeNode{}, fmt.Errorf("expected %s to be a union-type schema", field.Interface.TypeSpec.Name())
	}
	return union, nil
}

type InterfaceProp struct {
	Field                       syntax.StructField
	Interface                   syntax.IfaceImplementations
	DiscPropName                string
	InterfaceTypeNameWithPrefix string
	Optional                    bool
	Repeated                    bool
	EmbeddedPath                []EmbeddedField
}

type EmbeddedField struct {
	Name     string
	TypeName string
	Pointer  bool
}

func (s InterfaceProp) UnmarshalerFunc() string {
	identityHash := sha256.Sum256([]byte(s.helperIdentity()))
	return fmt.Sprintf(
		"__jsonUnmarshal__%s__%s__%s",
		s.Interface.TypeSpec.Pkg().Name,
		s.Interface.TypeSpec.Name(),
		hex.EncodeToString(identityHash[:]),
	)
}

func (s InterfaceProp) MarshalerFunc() string {
	return strings.Replace(s.UnmarshalerFunc(), "__jsonUnmarshal__", "__jsonMarshal__", 1)
}

// helperIdentity names the exact resolved interface registration consumed by a
// generated helper. Length-prefixed parts keep distinct package paths and
// configurations unambiguous even when package and type names match.
func (s InterfaceProp) helperIdentity() string {
	var identity strings.Builder
	writePart := func(value string) {
		fmt.Fprintf(&identity, "%d:%s;", len(value), value)
	}
	writePart(s.Interface.TypeSpec.Pkg().PkgPath)
	writePart(s.Interface.TypeSpec.Name())
	writePart(s.DiscPropName)
	for _, impl := range s.Interface.Impls {
		writePart(impl.PkgPath)
		writePart(impl.TypeName)
		writePart(strconv.Itoa(int(impl.Indirection)))
	}
	return identity.String()
}

func (i InterfaceProp) FieldNames() string {
	var names []string
	for _, name := range i.Field.Field.Names {
		names = append(names, name.Name)
	}
	return strings.Join(names, ", ")
}

func (i InterfaceProp) StructTag() string {
	if i.Field.Field.Tag == nil {
		return ""
	}
	return i.Field.Field.Tag.Value
}

func (i InterfaceProp) JSONName() string {
	names := i.Field.PropNames()
	if len(names) == 0 {
		return i.FieldNames()
	}
	return names[0]
}

func (i InterfaceProp) Accessor(receiver string) string {
	parts := []string{receiver}
	for _, embedded := range i.EmbeddedPath {
		parts = append(parts, embedded.Name)
	}
	parts = append(parts, i.FieldNames())
	return strings.Join(parts, ".")
}

func (i InterfaceProp) Accessible(receiver string) string {
	var checks []string
	parts := []string{receiver}
	for _, embedded := range i.EmbeddedPath {
		parts = append(parts, embedded.Name)
		if embedded.Pointer {
			checks = append(checks, strings.Join(parts, ".")+" != nil")
		}
	}
	if len(checks) == 0 {
		return "true"
	}
	return strings.Join(checks, " && ")
}

func (i InterfaceProp) HasPointerPath() bool {
	return slices.ContainsFunc(i.EmbeddedPath, func(field EmbeddedField) bool { return field.Pointer })
}

func (i InterfaceProp) PointerInitializers(receiver string) []string {
	var initializers []string
	parts := []string{receiver}
	for _, embedded := range i.EmbeddedPath {
		parts = append(parts, embedded.Name)
		if embedded.Pointer {
			initializers = append(initializers, fmt.Sprintf("if %s == nil { %s = new(%s) }", strings.Join(parts, "."), strings.Join(parts, "."), embedded.TypeName))
		}
	}
	return initializers
}

// resolveLocalInterfaceProps finds supported registered-interface properties on
// local structs. A direct interface and a direct []interface are supported;
// registered interfaces nested in any other container shape are rejected.
//
// Valid:
// ```
// type MyInterface interface{}
//
//	type struct Foo {
//	  Bar MyInterface `json:"bar"`
//	}
//
// ```
// Invalid examples:
// ```
// type (
//
//	MyInterface interface{}
//	struct Foo {
//	  Bar (MyInterface) `json:"bar"`
//	  Baz [][]MyInterface `json:"baz"`
//	  Bap struct { // Inline structs are permissible, but they cannot contain interfaces.
//	    Rap MyInterface `json:"rap"`
//	  }
//	}
//
// )
// ```
func (s SchemaBuilder) resolveLocalInterfaceProps(t syntax.StructType, seenProps syntax.SeenProps, embeddedPath []EmbeddedField) (props []InterfaceProp, err error) {
	if t.Pkg().PkgPath != s.Scan.Pkg.PkgPath {
		return nil, nil
	}
	for _, prop := range t.Fields() {
		if prop.Embedded() {
			continue
		}
		unseen := false
		for _, name := range prop.PropNames() {
			if !seenProps.Seen(name) {
				unseen = true
			}
			seenProps = seenProps.See(name)
		}
		if !unseen {
			continue
		}
		field, fieldErr := s.resolveRegisteredInterfaceField(t, prop)
		if fieldErr != nil {
			return nil, fieldErr
		}
		if field == nil {
			continue
		}
		if len(prop.Field.Names) != 1 {
			return nil, fmt.Errorf("interface prop %s has more than one field name at %s", strings.Join(prop.PropNames(), ","), prop.Position())
		}
		if err := validateInterfaceDiscriminators(t.Name(), prop.Field.Names[0].Name, *field); err != nil {
			return nil, err
		}
		props = append(props, InterfaceProp{
			Field:        prop,
			Interface:    field.Interface,
			DiscPropName: field.DiscPropName,
			Optional:     field.Optional,
			Repeated:     field.Repeated,
			EmbeddedPath: slices.Clone(embeddedPath),
		})
	}
	for _, prop := range t.Fields() {
		if !prop.Embedded() {
			continue
		}
		if _t, err := s.resolveEmbeddedType(prop.TypeExpr, nil); err != nil {
			return nil, fmt.Errorf("resolving embedded type: %w", err)
		} else if selector, ok := embeddedField(prop.Field.Type, _t.Name()); !ok {
			return nil, fmt.Errorf("unsupported embedded field type %T at %s", prop.Field.Type, prop.Position())
		} else if propsTemp, err := s.resolveLocalInterfaceProps(_t, seenProps, append(slices.Clone(embeddedPath), selector)); err != nil {
			return nil, fmt.Errorf("resolving embedded local interface properties: %w", err)
		} else {
			props = append(props, propsTemp...)
		}
	}
	return props, nil
}

func validateOwnerCodecInterfaceFields(owner string, props []InterfaceProp) error {
	goFields := make(map[string]string, len(props))
	jsonFields := make(map[string]string, len(props))
	for _, prop := range props {
		path := prop.Accessor(owner)
		goName := prop.FieldNames()
		if previous, exists := goFields[goName]; exists {
			return fmt.Errorf(
				"cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share Go field name %q",
				owner,
				previous,
				path,
				goName,
			)
		}
		goFields[goName] = path

		jsonName := prop.JSONName()
		if previous, exists := jsonFields[jsonName]; exists {
			return fmt.Errorf(
				"cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share JSON property %q",
				owner,
				previous,
				path,
				jsonName,
			)
		}
		jsonFields[jsonName] = path
	}
	return nil
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

func embeddedField(expr dst.Expr, typeName string) (EmbeddedField, bool) {
	field := EmbeddedField{TypeName: typeName}
	for {
		switch value := expr.(type) {
		case *dst.StarExpr:
			field.Pointer = true
			expr = value.X
		case *dst.ParenExpr:
			expr = value.X
		case *dst.Ident:
			field.Name = value.Name
			return field, true
		case *dst.SelectorExpr:
			field.Name = value.Sel.Name
			return field, true
		default:
			return EmbeddedField{}, false
		}
	}
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
