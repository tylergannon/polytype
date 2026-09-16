package builder

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/tylergannon/polytype/internal/common"
	"github.com/tylergannon/polytype/internal/schema"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// TypeDefinitions returns the builder's roots and their reachable named
// dependencies lowered into the validated, static type grammar, for the
// backends that need every wire shape to be static (TypeScript, devalue, the
// grammar package). It performs no rendering and does not mutate builder
// state or output files.
//
// Generation lowers once, permissively: a shape whose wire contract is
// supplied outside the grammar (a runtime schema provider, an explicit
// schema reference, a custom JSON codec) still lowers, so that JSON Schema
// and the Go codecs can be projected from the same model. Each such shape is
// recorded as a strict refusal, and this method reports the first one
// instead of returning definitions a strict backend must not consume.
func (s *SchemaBuilder) TypeDefinitions() (typegrammar.Definitions, error) {
	if s == nil {
		return nil, fmt.Errorf("build type definitions: nil SchemaBuilder")
	}
	if err := typeGrammarPackageError(s.Scan); err != nil {
		return nil, err
	}
	lowered := s.lowered
	if lowered == nil || len(s.selectedRoots) > 0 {
		// A partial selection (NewForTypes) lowered only its roots; the
		// grammar backends see every declared root, as they always have.
		var err error
		if lowered, err = s.lower(nil); err != nil {
			return nil, err
		}
	}
	if err := lowered.strictError(); err != nil {
		return nil, err
	}
	return lowered.defs, nil
}

// Refusal messages shared by the two lowering entry points. The dst-based
// field lowering (typ) and the go/types root bridge (rootType) must reject the
// same shape with the same words, so a caller cannot tell which path found it.
const (
	msgMapType             = "maps are outside the static type grammar at %s"
	msgChanType            = "channels are outside the static type grammar at %s"
	msgFuncType            = "functions are outside the static type grammar at %s"
	msgInterfaceType       = "interfaces are valid only as configured direct fields at %s"
	msgPresenceWrapper     = "presence wrappers are valid only as complete direct named fields at %s"
	msgUnsupportedTypeExpr = "unsupported type expression %T at %s"
	msgUnresolvedExternal  = "unresolved external type %s; no static wire shape was loaded"
	msgRegisteredInterface = "registered interface %s is valid only as a configured direct field"
)

// lowering is the result of one pass over a set of roots: the definition
// graph every backend consumes, plus the facts about it that only Go code
// generation needs and the grammar deliberately does not carry.
type lowering struct {
	defs typegrammar.Definitions
	// roots holds one entry per lowered root, in root order.
	roots []loweredRoot
	// strict lists every shape that lowered permissively but that a strict
	// backend must refuse, in discovery order. The first is authoritative.
	strict []error
	// fields records, per named object owner and Go field name, the source
	// facts the generated owner codecs need: the field's struct tag and the
	// embedded path it was promoted through.
	fields map[typegrammar.Name]map[string]fieldSource
}

type loweredRoot struct {
	method syntax.SchemaMethod
	schema schema.Root
}

// fieldSource is what the Go codec generator needs to name and tag a field
// that the grammar has already resolved.
type fieldSource struct {
	tag string
	// path is the chain of embedded field names the field was promoted
	// through, empty for a direct field.
	path []string
	// steps and order reproduce the generator's field order: a struct's own
	// fields first, then each embedded struct's in declaration order.
	steps []int
	order int
	// provided is the static value behind a field whose JSON Schema is
	// supplied outside the grammar (an explicit ref or a runtime provider),
	// when that value lowers. JSON Schema renders what was supplied; the Go
	// codecs still adapt the field by its Go type.
	provided typegrammar.FieldValue
}

func (l *lowering) strictError() error {
	if len(l.strict) == 0 {
		return nil
	}
	return l.strict[0]
}

// strict is the lowerer's own view of strictError, for the entry points that
// lower without building a lowering value.
func (l *typeGrammarLowerer) strictErr() error {
	if len(l.strict) == 0 {
		return nil
	}
	return l.strict[0]
}

// lower lowers every selected root of the builder. It is the single lowering
// generation performs: JSON Schema, Go codecs and the grammar backends all
// consume its definitions.
func (s *SchemaBuilder) lower(typeNames []string) (*lowering, error) {
	l := typeGrammarLowerer{
		builder: s,
		index:   make(map[typegrammar.Name]int),
		resolve: func(name typegrammar.Name) error {
			return s.Scan.EnsureRemoteType(name.PackagePath, name.Name)
		},
		fields: make(map[typegrammar.Name]map[string]fieldSource),
	}
	var roots []loweredRoot
	for _, root := range s.roots() {
		if !selectedRoot(typeNames, root.Receiver.TypeName) {
			continue
		}
		name := typegrammar.Name{PackagePath: root.Receiver.PkgPath, Name: root.Receiver.TypeName}
		lowered, err := l.root(name)
		if err != nil {
			return nil, fmt.Errorf("build type definitions for %s: %w", name, err)
		}
		roots = append(roots, loweredRoot{method: root, schema: lowered})
	}
	if err := l.defs.Validate(); err != nil {
		return nil, fmt.Errorf("validate type definitions: %w", err)
	}
	return &lowering{defs: l.defs, roots: roots, strict: l.strict, fields: l.fields}, nil
}

type typeGrammarLowerer struct {
	builder *SchemaBuilder
	defs    typegrammar.Definitions
	index   map[typegrammar.Name]int
	// resolve, when set, loads a package that marker-seeded traversal never
	// reached.
	resolve func(name typegrammar.Name) error
	strict  []error
	fields  map[typegrammar.Name]map[string]fieldSource
}

// refuse records a shape the strict backends must not consume. Generation
// keeps lowering it, because JSON Schema and the Go codecs can still be
// projected around it.
func (l *typeGrammarLowerer) refuse(format string, args ...any) {
	l.strict = append(l.strict, fmt.Errorf(format, args...))
}

// errPackageNotLoaded reports that name's package is absent and no resolver
// could supply it. Each caller words that outcome for its own context.
var errPackageNotLoaded = errors.New("package not loaded")

// ensurePackage loads name's package on demand, if this lowerer was given a
// resolver. A resolver failure is returned as-is: it names a real load error,
// which is more useful than "not loaded".
func (l *typeGrammarLowerer) ensurePackage(name typegrammar.Name) error {
	if _, ok := l.builder.Scan.GetPackage(name.PackagePath); ok {
		return nil
	}
	if l.resolve == nil {
		return errPackageNotLoaded
	}
	if err := l.resolve(name); err != nil {
		return fmt.Errorf("load package %q for named type %s: %w", name.PackagePath, name, err)
	}
	if _, ok := l.builder.Scan.GetPackage(name.PackagePath); !ok {
		return errPackageNotLoaded
	}
	return nil
}

// root lowers one declared root. A sealed interface root is the one shape
// the grammar admits only as a field: it lowers as the union of its variants
// for JSON Schema, and is a strict refusal for every other backend.
func (l *typeGrammarLowerer) root(name typegrammar.Name) (schema.Root, error) {
	if err := l.ensurePackage(name); err != nil {
		if !errors.Is(err, errPackageNotLoaded) {
			return schema.Root{}, err
		}
		return schema.Root{}, fmt.Errorf("unresolved package %q for named type %s", name.PackagePath, name)
	}
	scan, _ := l.builder.Scan.GetPackage(name.PackagePath)
	if iface, ok := scan.Interfaces[name.Name]; ok {
		l.refuse(msgRegisteredInterface, name)
		union, err := l.union(registeredInterfaceField{Interface: iface, DiscPropName: iface.Discriminator})
		if err != nil {
			return schema.Root{}, err
		}
		return schema.Root{Union: &union}, nil
	}
	if err := l.named(name); err != nil {
		return schema.Root{}, err
	}
	return schema.Root{Name: name}, nil
}

func (l *typeGrammarLowerer) named(name typegrammar.Name) error {
	if _, ok := l.index[name]; ok {
		return nil
	}
	if err := l.ensurePackage(name); err != nil {
		if !errors.Is(err, errPackageNotLoaded) {
			return err
		}
		return fmt.Errorf("unresolved package %q for named type %s", name.PackagePath, name)
	}
	scan, _ := l.builder.Scan.GetPackage(name.PackagePath)
	if err := typeGrammarPackageError(scan); err != nil {
		// Generation has always rendered a package that fails to type-check,
		// from whatever go/types could still resolve. A backend that derives
		// every wire shape from go/types facts cannot trust such a package.
		l.strict = append(l.strict, err)
	}

	var typeSpec syntax.TypeSpec
	var enumSet *syntax.EnumSet
	if enumSet = scan.Constants[name.Name]; enumSet != nil {
		typeSpec = enumSet.TypeSpec
	} else {
		var found bool
		typeSpec, found = scan.LocalNamedTypes[name.Name]
		if !found {
			if _, isInterface := scan.Interfaces[name.Name]; isInterface {
				return sealedUnionMisuse{fmt.Errorf(msgRegisteredInterface, name)}
			}
			return fmt.Errorf("unresolved named type %s", name)
		}
	}

	idx := len(l.defs)
	l.index[name] = idx
	l.defs = append(l.defs, typegrammar.Definition{
		Name:        name,
		Description: typeSpec.Comments(),
		Source:      typeSpec.Position(),
	})

	if err := rejectCustomWireType(scan, name.Name, typeSpec.Position().String()); err != nil {
		// The Go codec honors the custom mapping and JSON Schema documents
		// the static shape, as they always have. Only a backend that must
		// derive the wire form statically has to refuse it.
		l.strict = append(l.strict, err)
	}
	var (
		typ typegrammar.Type
		err error
	)
	if enumSet != nil {
		typ, err = l.enum(enumSet, typegrammar.EnumValues)
	} else {
		typ, err = l.definitionType(name, typeSpec)
	}
	if err != nil {
		return fmt.Errorf("type %s at %s: %w", name, typeSpec.Position(), err)
	}
	l.defs[idx].Type = typ
	return nil
}

func (l *typeGrammarLowerer) definitionType(name typegrammar.Name, typeSpec syntax.TypeSpec) (typegrammar.Type, error) {
	if object, ok := typeSpec.Type().Expr().(*dst.StructType); ok {
		fields, err := l.structFields(name, syntax.NewStructType(object, typeSpec))
		if err != nil {
			return nil, err
		}
		return &typegrammar.Object{Fields: fields}, nil
	}
	return l.typ(typeSpec.Derive())
}

func (l *typeGrammarLowerer) typ(expr syntax.TypeExpr) (typegrammar.Type, error) {
	switch node := expr.Excerpt.(type) {
	case *dst.Ident:
		if kind, ok := scalarKind(node.Name); ok && node.Path == "" {
			return &typegrammar.Scalar{Kind: kind}, nil
		}
		pkgPath := node.Path
		if pkgPath == "" {
			pkgPath = expr.Pkg().PkgPath
		}
		if syntax.IsTimeType(pkgPath, node.Name) {
			return &typegrammar.Time{}, nil
		}
		name := typegrammar.Name{PackagePath: pkgPath, Name: node.Name}
		if err := l.ensurePackage(name); err != nil {
			if !errors.Is(err, errPackageNotLoaded) {
				return nil, err
			}
			return nil, fmt.Errorf(msgUnresolvedExternal, name)
		}
		if err := l.named(name); err != nil {
			return nil, err
		}
		return &typegrammar.Ref{Target: name}, nil

	case *dst.SelectorExpr:
		ident, err := l.selectorIdent(expr, node)
		if err != nil {
			return nil, err
		}
		return l.typ(expr.Derive(ident))

	case *dst.StarExpr:
		element, err := l.typ(expr.Derive(node.X))
		if err != nil {
			return nil, err
		}
		return &typegrammar.Pointer{Element: element}, nil

	case *dst.ParenExpr:
		return l.typ(expr.Derive(node.X))

	case *dst.ArrayType:
		if l.isRegisteredInterface(expr.Derive(node.Elt)) {
			// The grammar admits a union only as a direct field or a direct
			// one-dimensional slice of one; every other container is the
			// same refusal the field lowering reports.
			return nil, sealedUnionMisuse{fmt.Errorf("%s at %s", unsupportedRegisteredInterfaceContainer, expr.Position())}
		}
		element, err := l.typ(expr.Derive(node.Elt))
		if err != nil {
			return nil, err
		}
		if node.Len == nil {
			return &typegrammar.Slice{Element: element}, nil
		}
		length, err := arrayLength(expr, node)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Array{Length: length, Element: element}, nil

	case *dst.StructType:
		owner := syntax.NewStructType(node, *expr.TypeSpec)
		fields, err := l.anonymousStructFields(owner)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Object{Fields: fields}, nil

	case *dst.MapType:
		return nil, fmt.Errorf(msgMapType, expr.Position())
	case *dst.ChanType:
		return nil, fmt.Errorf(msgChanType, expr.Position())
	case *dst.FuncType:
		return nil, fmt.Errorf(msgFuncType, expr.Position())
	case *dst.InterfaceType:
		return nil, fmt.Errorf(msgInterfaceType, expr.Position())
	case *dst.IndexExpr, *dst.IndexListExpr:
		return nil, fmt.Errorf(msgPresenceWrapper, expr.Position())
	default:
		return nil, fmt.Errorf(msgUnsupportedTypeExpr, node, expr.Position())
	}
}

// selectorIdent resolves pkg.Name to a path-qualified identifier.
func (l *typeGrammarLowerer) selectorIdent(expr syntax.TypeExpr, node *dst.SelectorExpr) (*dst.Ident, error) {
	prefix, ok := node.X.(*dst.Ident)
	if !ok {
		return nil, fmt.Errorf("unsupported selector expression %T at %s", node.X, expr.Position())
	}
	pkgPath := prefix.Path
	if pkgPath == "" {
		pkgPath, _ = expr.Imports().GetPackageForPrefix(prefix.Name)
	}
	if pkgPath == "" {
		return nil, fmt.Errorf("could not resolve package prefix %q at %s", prefix.Name, expr.Position())
	}
	ident := dst.NewIdent(node.Sel.Name)
	ident.Path = pkgPath
	return ident, nil
}

// isRegisteredInterface reports whether expr names a sealed interface,
// through any pointers or parentheses.
func (l *typeGrammarLowerer) isRegisteredInterface(expr syntax.TypeExpr) bool {
	switch node := expr.Excerpt.(type) {
	case *dst.StarExpr:
		return l.isRegisteredInterface(expr.Derive(node.X))
	case *dst.ParenExpr:
		return l.isRegisteredInterface(expr.Derive(node.X))
	case *dst.Ident:
		_, ok := l.builder.findInterfaceImpl(node, expr.Pkg())
		return ok
	case *dst.SelectorExpr:
		ident, err := l.selectorIdent(expr, node)
		if err != nil {
			return false
		}
		_, ok := l.builder.findInterfaceImpl(ident, expr.Pkg())
		return ok
	default:
		return false
	}
}

// structFields lowers a named object's fields, recording the source facts
// the generated owner codec needs and checking the owner's field-level enum
// registrations against the fields that exist.
func (l *typeGrammarLowerer) structFields(name typegrammar.Name, owner syntax.StructType) ([]typegrammar.Field, error) {
	var (
		candidates []typeGrammarFieldCandidate
		order      int
	)
	if err := l.collectStructFields(owner, true, embeddedAt{}, &order, &candidates); err != nil {
		return nil, err
	}
	winners, ambiguous := dominantFieldCandidates(candidates)
	if err := l.rejectAmbiguousInterfaceFields(owner, ambiguous); err != nil {
		return nil, err
	}
	if err := rejectShadowedGoNames(owner, winners); err != nil {
		return nil, err
	}
	if err := l.checkEnumRegistrations(owner); err != nil {
		return nil, err
	}
	fields, provided, err := l.lowerFields(winners)
	if err != nil {
		return nil, err
	}
	sources := make(map[string]fieldSource, len(winners))
	for _, candidate := range winners {
		sources[candidate.name.goName] = fieldSource{
			tag:      candidate.tag(),
			path:     candidate.at.path,
			steps:    candidate.at.steps,
			order:    candidate.order,
			provided: provided[candidate.name.goName],
		}
	}
	if l.fields == nil {
		l.fields = make(map[typegrammar.Name]map[string]fieldSource)
	}
	l.fields[name] = sources
	return fields, nil
}

// anonymousStructFields lowers an inline struct. It is not a named owner, so
// it carries no registrations: no enum adaptation, no unions, no providers.
func (l *typeGrammarLowerer) anonymousStructFields(owner syntax.StructType) ([]typegrammar.Field, error) {
	var (
		candidates []typeGrammarFieldCandidate
		order      int
	)
	if err := l.collectStructFields(owner, false, embeddedAt{}, &order, &candidates); err != nil {
		return nil, err
	}
	winners, _ := dominantFieldCandidates(candidates)
	if err := rejectShadowedGoNames(owner, winners); err != nil {
		return nil, err
	}
	fields, _, err := l.lowerFields(winners)
	return fields, err
}

// rejectShadowedGoNames refuses an owner whose flattened fields share a Go
// field name: a promoted field shadowed by a shallower field of the same
// name but a different JSON name. encoding/json serializes both, but the
// grammar's objects are flat, so the promoted field has no Go name of its
// own to be addressed by.
func rejectShadowedGoNames(owner syntax.StructType, winners []typeGrammarFieldCandidate) error {
	byGoName := make(map[string]typeGrammarFieldCandidate, len(winners))
	for _, candidate := range winners {
		first, ok := byGoName[candidate.name.goName]
		if !ok {
			byGoName[candidate.name.goName] = candidate
			continue
		}
		shallow, deep := first, candidate
		if deep.depth() < shallow.depth() {
			shallow, deep = deep, shallow
		}
		return fmt.Errorf("promoted field %s shares Go field name %q with %s at %s; a shadowed promoted field is outside the static type grammar",
			deep.accessor(owner.Name()), deep.name.goName, shallow.accessor(owner.Name()), deep.field.Position())
	}
	return nil
}

// lowerFields lowers each winning candidate. The second result maps a Go
// field name to the static value behind its Provided form, where one lowered.
func (l *typeGrammarLowerer) lowerFields(winners []typeGrammarFieldCandidate) ([]typegrammar.Field, map[string]typegrammar.FieldValue, error) {
	fields := make([]typegrammar.Field, 0, len(winners))
	provided := make(map[string]typegrammar.FieldValue)
	for _, candidate := range winners {
		value, behind, err := l.fieldValue(candidate)
		if err != nil {
			return nil, nil, err
		}
		if behind != nil {
			provided[candidate.name.goName] = behind
		}
		fields = append(fields, typegrammar.Field{
			GoName:      candidate.name.goName,
			JSONName:    candidate.name.json,
			Value:       value,
			Description: candidate.field.Comments(),
			Source:      candidate.field.Position(),
		})
	}
	return fields, provided, nil
}

// embeddedAt locates an embedded struct being flattened into its outermost
// owner: the embedded field names from that owner, and each one's index
// among its struct's fields.
type embeddedAt struct {
	path  []string
	steps []int
}

func (at embeddedAt) into(index int, name string) embeddedAt {
	return embeddedAt{
		path:  append(slices.Clone(at.path), name),
		steps: append(slices.Clone(at.steps), index),
	}
}

type typeGrammarFieldCandidate struct {
	owner      syntax.StructType
	field      syntax.StructField
	name       resolvedFieldName
	at         embeddedAt
	order      int
	tagged     bool
	namedOwner bool
}

func (c typeGrammarFieldCandidate) depth() int { return len(c.at.steps) }

func (c typeGrammarFieldCandidate) tag() string {
	if c.field.Field.Tag == nil {
		return ""
	}
	return c.field.Field.Tag.Value
}

// accessor is the Go selector path of the field from a value of the
// outermost owner: Owner.Embedded.Field for a promoted field.
func (c typeGrammarFieldCandidate) accessor(receiver string) string {
	parts := append([]string{receiver}, c.at.path...)
	return strings.Join(append(parts, c.name.goName), ".")
}

func (l *typeGrammarLowerer) collectStructFields(owner syntax.StructType, namedOwner bool, at embeddedAt, order *int, candidates *[]typeGrammarFieldCandidate) error {
	for index, field := range owner.Fields() {
		if field.Embedded() && !hasExplicitJSONName(field) {
			wrapper, _, err := field.Wrapper()
			if err != nil {
				return err
			}
			if err := validateStaticFieldWireContract(owner, field, wrapper); err != nil {
				return err
			}
			embedded, err := l.builder.resolveEmbeddedType(field.TypeExpr, nil)
			if err != nil {
				return err
			}
			if err := l.recordEmbeddedDependency(field.TypeExpr); err != nil {
				return fmt.Errorf("embedded field at %s: %w", field.Position(), err)
			}
			name, _ := embeddedFieldName(field)
			if err := l.collectStructFields(embedded, true, at.into(index, name), order, candidates); err != nil {
				return err
			}
			continue
		}
		names := resolvedFieldNames(field)
		if field.Embedded() {
			name, ok := embeddedFieldName(field)
			if !ok {
				return fmt.Errorf("unsupported embedded field at %s", field.Position())
			}
			jsonName := name
			if tag := field.JSONTag(); tag != nil && tag.Options[0] != "" {
				jsonName = tag.Options[0]
			}
			names = []resolvedFieldName{{goName: name, json: jsonName}}
		}
		for _, name := range names {
			*candidates = append(*candidates, typeGrammarFieldCandidate{
				owner: owner, field: field, name: name, at: at, order: *order,
				tagged: hasExplicitJSONName(field), namedOwner: namedOwner,
			})
			*order++
		}
	}
	return nil
}

// dominantFieldCandidates applies encoding/json's dominance rule per JSON
// name: the shallowest candidates win, and among several at the same depth
// only a single tagged one does. A name with no winner is dropped, as
// encoding/json drops it; those groups are returned so a caller can refuse
// the ones it must not drop silently.
func dominantFieldCandidates(candidates []typeGrammarFieldCandidate) (winners, ambiguous []typeGrammarFieldCandidate) {
	byName := make(map[string][]typeGrammarFieldCandidate)
	for _, candidate := range candidates {
		byName[candidate.name.json] = append(byName[candidate.name.json], candidate)
	}
	for _, group := range byName {
		minDepth := group[0].depth()
		for _, candidate := range group[1:] {
			if candidate.depth() < minDepth {
				minDepth = candidate.depth()
			}
		}
		var shallow []typeGrammarFieldCandidate
		for _, candidate := range group {
			if candidate.depth() == minDepth {
				shallow = append(shallow, candidate)
			}
		}
		if len(shallow) == 1 {
			winners = append(winners, shallow[0])
			continue
		}
		var tagged []typeGrammarFieldCandidate
		for _, candidate := range shallow {
			if candidate.tagged {
				tagged = append(tagged, candidate)
			}
		}
		if len(tagged) == 1 {
			winners = append(winners, tagged[0])
			continue
		}
		ambiguous = append(ambiguous, shallow...)
	}
	byOrder := func(a, b typeGrammarFieldCandidate) int { return a.order - b.order }
	slices.SortFunc(winners, byOrder)
	slices.SortFunc(ambiguous, byOrder)
	return winners, ambiguous
}

// rejectAmbiguousInterfaceFields refuses an owner whose promoted registered
// interface fields cancel each other out. encoding/json would drop the
// property; a generated codec that silently did the same would lose a union
// the author declared.
func (l *typeGrammarLowerer) rejectAmbiguousInterfaceFields(owner syntax.StructType, ambiguous []typeGrammarFieldCandidate) error {
	if owner.Pkg().PkgPath != l.builder.Scan.Pkg.PkgPath {
		return nil
	}
	for i, candidate := range ambiguous {
		if !l.isRegisteredInterfaceField(candidate) {
			continue
		}
		for _, other := range ambiguous[i+1:] {
			if other.name.json != candidate.name.json {
				continue
			}
			if other.name.goName == candidate.name.goName {
				return fmt.Errorf("cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share Go field name %q", owner.Name(), candidate.accessor(owner.Name()), other.accessor(owner.Name()), candidate.name.goName)
			}
			return fmt.Errorf("cannot generate owner codec for %s: promoted registered interface fields %s and %s are ambiguous because they share JSON property %q", owner.Name(), candidate.accessor(owner.Name()), other.accessor(owner.Name()), candidate.name.json)
		}
	}
	return nil
}

func (l *typeGrammarLowerer) isRegisteredInterfaceField(candidate typeGrammarFieldCandidate) bool {
	fieldType := candidate.field.Type()
	if wrapper, inner, err := candidate.field.Wrapper(); err == nil && wrapper != syntax.WrapperNone {
		fieldType = inner
	}
	ident, _, ok := directInterfaceFieldType(fieldType)
	if !ok {
		return false
	}
	_, ok = l.builder.findInterfaceImpl(ident, candidate.field.Pkg())
	return ok
}

// checkEnumRegistrations verifies every .StringerEnum registration on owner
// names one of its own single-name JSON fields.
func (l *typeGrammarLowerer) checkEnumRegistrations(owner syntax.StructType) error {
	if owner.Pkg().PkgPath != l.builder.Scan.Pkg.PkgPath {
		return nil
	}
	configs := l.builder.EnumV1[owner.Name()]
	if len(configs) == 0 {
		return nil
	}
	direct := make(map[string]syntax.StructField)
	for _, field := range owner.Fields() {
		for _, name := range field.Field.Names {
			direct[name.Name] = field
		}
	}
	for _, fieldName := range slices.Sorted(maps.Keys(configs)) {
		field, ok := direct[fieldName]
		if !ok {
			return fmt.Errorf("field %s.%s: registered enum field was not found", owner.Name(), fieldName)
		}
		if len(field.Field.Names) != 1 || field.Skip() {
			return fmt.Errorf("field %s.%s: registered enum must be a single JSON field", owner.Name(), fieldName)
		}
	}
	return nil
}

// fieldValue lowers one field. A field with an explicit schema ref lowers to
// the Provided form whatever its Go type: the author's ref wins over the
// union or enum the type would otherwise render as. The second result is the
// static value behind a Provided field, when one lowers.
func (l *typeGrammarLowerer) fieldValue(c typeGrammarFieldCandidate) (typegrammar.FieldValue, typegrammar.FieldValue, error) {
	owner, field := c.owner, c.field
	ref, hasRef := explicitSchemaRef(field)
	if !hasRef {
		return l.staticFieldValue(c, true)
	}
	wrapper, _, err := field.Wrapper()
	if err != nil {
		return nil, nil, err
	}
	if err := l.validateProvidedField(c, wrapper, "explicit refs"); err != nil {
		return nil, nil, err
	}
	l.refuse("field %s.%s at %s uses an explicit schema ref with no resolved static type target", owner.Name(), fieldName(field), field.Position())
	behind, err := l.speculate(c)
	if err != nil {
		return nil, nil, err
	}
	return &typegrammar.Provided{Ref: ref, Optional: wrapper == syntax.WrapperOptional}, behind, nil
}

// validateProvidedField applies the field rules that hold whatever supplies
// the field's schema.
func (l *typeGrammarLowerer) validateProvidedField(c typeGrammarFieldCandidate, wrapper syntax.WrapperKind, supplied string) error {
	owner, field := c.owner, c.field
	if err := validateStaticFieldWireContract(owner, field, wrapper); err != nil {
		return err
	}
	if wrapper == syntax.WrapperOptional && !field.HasJSONOption("omitzero") {
		return fmt.Errorf("%s field %s.%s requires json:\",omitzero\" at %s", wrapper, owner.Name(), fieldName(field), field.Position())
	}
	if wrapper == syntax.WrapperNullable {
		return fmt.Errorf("%s does not support %s at %s", wrapper, supplied, field.Position())
	}
	return nil
}

// staticFieldValue lowers a field from its Go type and registrations. With
// withProviders false, a registered runtime provider is ignored, which is how
// speculate finds the value behind one.
func (l *typeGrammarLowerer) staticFieldValue(c typeGrammarFieldCandidate, withProviders bool) (typegrammar.FieldValue, typegrammar.FieldValue, error) {
	owner, field := c.owner, c.field
	if c.namedOwner && c.depth() == 0 && owner.Pkg().PkgPath == l.builder.Scan.Pkg.PkgPath {
		if cfg, ok := l.enumConfig(owner, field); ok {
			value, err := l.registeredEnumField(owner, field, cfg)
			return value, nil, err
		}
	}

	wrapper, inner, err := field.Wrapper()
	if err != nil {
		return nil, nil, err
	}
	if err := validateStaticFieldWireContract(owner, field, wrapper); err != nil {
		return nil, nil, err
	}
	if wrapper == syntax.WrapperOptional && !field.HasJSONOption("omitzero") {
		return nil, nil, fmt.Errorf("%s field %s.%s requires json:\",omitzero\" at %s", wrapper, owner.Name(), fieldName(field), field.Position())
	}

	if c.namedOwner {
		interfaceField, err := l.builder.resolveRegisteredInterfaceField(owner, field)
		if err != nil {
			return nil, nil, sealedUnionMisuse{err}
		}
		if interfaceField != nil {
			if err := validateInterfaceDiscriminators(owner.Name(), fieldName(field), *interfaceField); err != nil {
				return nil, nil, sealedUnionMisuse{err}
			}
			union, err := l.union(*interfaceField)
			if err != nil {
				return nil, nil, err
			}
			switch {
			case interfaceField.Repeated:
				return &typegrammar.UnionSlice{Union: union}, nil, nil
			case interfaceField.Optional:
				return &typegrammar.OptionalUnion{Union: union}, nil, nil
			default:
				return &union, nil, nil
			}
		}
	}

	// Providers are registered on a named owner; an inline struct's fields
	// reuse the enclosing type's name but carry none of its registrations.
	if providers := l.builder.TypeProvidersMap[owner.Name()]; withProviders && c.namedOwner && hasProviderForGoField(providers, goFieldNames(field)) {
		if wrapper == syntax.WrapperNullable {
			return nil, nil, fmt.Errorf("%s does not support providers at %s", wrapper, field.Position())
		}
		l.refuse("field %s.%s at %s uses a runtime schema provider with no statically resolved wire type", owner.Name(), fieldName(field), field.Position())
		behind, err := l.speculate(c)
		if err != nil {
			return nil, nil, err
		}
		return &typegrammar.Provided{Optional: wrapper == syntax.WrapperOptional}, behind, nil
	}
	if tag := field.JSONTag(); tag != nil && slices.Contains(tag.Options[1:], "string") {
		l.refuse("field %s.%s at %s uses json:\",string\", whose wire mapping is outside the static type grammar", owner.Name(), fieldName(field), field.Position())
	}

	renderType := field.Type()
	if wrapper != syntax.WrapperNone {
		renderType = inner
	}
	if wrapper == syntax.WrapperNullable {
		if _, isArray := renderType.(*dst.ArrayType); isArray {
			return nil, nil, fmt.Errorf("%s does not support arrays/slices at %s", wrapper, field.Position())
		}
	}
	typ, err := l.typ(field.Derive(renderType))
	if err != nil {
		return nil, nil, fmt.Errorf("field %s.%s: %w", owner.Name(), fieldName(field), err)
	}
	value, err := wrapFieldValue(wrapper, typ)
	return value, nil, err
}

// speculate lowers the static value behind a field whose schema is supplied
// outside the grammar. The supplied schema makes the static shape optional,
// so a shape the grammar cannot lower is not an error here: everything the
// attempt added is rolled back and the field simply has no static value. A
// misused sealed union is still an error, because the generated Go codecs
// adapt the field by its Go type whatever schema was supplied, and cannot
// adapt that shape.
func (l *typeGrammarLowerer) speculate(c typeGrammarFieldCandidate) (typegrammar.FieldValue, error) {
	defs, strict := len(l.defs), len(l.strict)
	value, _, err := l.staticFieldValue(c, false)
	// The field is already refused for the strict backends; what the attempt
	// found beyond that is not authoritative.
	l.strict = l.strict[:strict]
	if err == nil {
		return value, nil
	}
	for name, idx := range l.index {
		if idx >= defs {
			delete(l.index, name)
			delete(l.fields, name)
		}
	}
	l.defs = l.defs[:defs]
	if errors.As(err, new(sealedUnionMisuse)) {
		return nil, err
	}
	return nil, nil
}

// sealedUnionMisuse marks a refusal of a sealed-union field shape: one the
// generated codecs cannot adapt, whatever schema the field is given.
type sealedUnionMisuse struct{ error }

func (e sealedUnionMisuse) Unwrap() error { return e.error }

func explicitSchemaRef(field syntax.StructField) (string, bool) {
	if field.Field.Tag == nil {
		return "", false
	}
	tag := common.ParseJSONSchemaTag(field.Field.Tag.Value)
	return tag.Ref, tag.HasRef
}

// registeredEnumField lowers a field carrying a .StringerEnum registration.
// An integer enum is adapted to its constant names on the wire (EnumNames);
// a string enum keeps its values. Either way the field renders the enum
// inline, and the enum type is also lowered as a reusable definition.
func (l *typeGrammarLowerer) registeredEnumField(owner syntax.StructType, field syntax.StructField, cfg enumFieldConfig) (typegrammar.FieldValue, error) {
	name := fieldName(field)
	if field.HasJSONOption("string") {
		return nil, fmt.Errorf("field %s.%s: registered enum fields do not support json:\",string\" at %s", owner.Name(), name, field.Position())
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
		return nil, fmt.Errorf("field %s.%s: .StringerEnum supports only a direct named enum, Optional[E], or Nullable[E] at %s", owner.Name(), name, field.Position())
	}
	if err := validateStaticFieldWireContract(owner, field, wrapper); err != nil {
		return nil, err
	}
	if wrapper == syntax.WrapperOptional && !field.HasJSONOption("omitzero") {
		return nil, fmt.Errorf("%s field %s.%s requires json:\",omitzero\" at %s", wrapper, owner.Name(), name, field.Position())
	}
	if interfaceField, err := l.builder.resolveRegisteredInterfaceField(owner, field); err != nil {
		return nil, err
	} else if interfaceField != nil {
		return nil, fmt.Errorf("field %s.%s cannot be both an enum and registered interface", owner.Name(), name)
	}

	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = field.Pkg().PkgPath
	}
	scan, ok := l.builder.Scan.GetPackage(pkgPath)
	if !ok {
		return nil, fmt.Errorf("field %s.%s: could not resolve enum package %s", owner.Name(), name, pkgPath)
	}
	enumSet := scan.Constants[ident.Name]
	if enumSet == nil {
		typeSpec, found := scan.LocalNamedTypes[ident.Name]
		if !found {
			return nil, fmt.Errorf("field %s.%s: could not resolve enum type %s", owner.Name(), name, ident.Name)
		}
		if enumSet, err = syntax.ResolveEnum(typeSpec); err != nil {
			return nil, fmt.Errorf("field %s.%s: resolving enum type %s: %w", owner.Name(), name, ident.Name, err)
		}
	}
	if enumSet == nil || len(enumSet.Values) == 0 {
		return nil, fmt.Errorf("field %s.%s: no constants declared for enum type %s", owner.Name(), name, ident.Name)
	}
	kind, err := enumScalarKind(enumSet)
	if err != nil {
		return nil, fmt.Errorf("field %s.%s: enum type %s must have an integer or string underlying type", owner.Name(), name, ident.Name)
	}
	enumName := typegrammar.Name{PackagePath: enumSet.TypeSpec.Pkg().PkgPath, Name: enumSet.TypeSpec.Name()}
	if err := l.named(enumName); err != nil {
		return nil, err
	}
	adapted := cfg.UseStringer && kind != typegrammar.String
	if adapted {
		methods, err := syntax.FindProductionJSONMethods(scan.Pkg.Dir, []string{ident.Name})
		if err != nil {
			return nil, fmt.Errorf("field %s.%s: discovering enum JSON methods: %w", owner.Name(), name, err)
		}
		if len(methods) > 0 {
			return nil, fmt.Errorf("field %s.%s: cannot adapt string-mode enum %s because production %s is declared at %s", owner.Name(), name, ident.Name, methods[0].Name, methods[0].Position)
		}
	}
	remote := scan.Pkg.PkgPath != l.builder.Scan.Pkg.PkgPath
	members, err := registeredEnumMembers(enumSet, adapted, remote)
	if err != nil {
		return nil, fmt.Errorf("field %s.%s: %w", owner.Name(), name, err)
	}
	mode := typegrammar.EnumValues
	if adapted {
		mode = typegrammar.EnumNames
	}
	return wrapFieldValue(wrapper, &typegrammar.Enum{GoType: enumName, Kind: kind, Mode: mode, Members: members})
}

// registeredEnumMembers lists a registered enum's members with one entry per
// wire value. A value-mode string enum keeps the first constant of each
// value; a name-mode integer enum cannot decode an ambiguous value at all.
func registeredEnumMembers(enumSet *syntax.EnumSet, adapted, remote bool) ([]typegrammar.EnumMember, error) {
	seen := make(map[string]string, len(enumSet.Values))
	members := make([]typegrammar.EnumMember, 0, len(enumSet.Values))
	for _, member := range enumSet.Values {
		if remote && adapted && !token.IsExported(member.Name) {
			return nil, fmt.Errorf("string-mode enum constant %s is not exported", member.Name)
		}
		exact := member.Value.ExactString()
		if previous, exists := seen[exact]; exists && previous != member.Name {
			if adapted {
				return nil, fmt.Errorf("enum constants %s and %s have duplicate underlying value %s", previous, member.Name, exact)
			}
			continue
		}
		seen[exact] = member.Name
		members = append(members, typegrammar.EnumMember{Name: member.Name, Value: member.Value, Description: member.Description})
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("enum %s has no declared constants", enumSet.TypeSpec.Name())
	}
	return members, nil
}

func validateStaticFieldWireContract(owner syntax.StructType, field syntax.StructField, wrapper syntax.WrapperKind) error {
	if wrapper != syntax.WrapperNone {
		return nil
	}
	name := fieldName(field)
	if name == "" {
		name, _ = embeddedFieldName(field)
	}
	for _, option := range []string{"omitempty", "omitzero"} {
		if field.HasJSONOption(option) {
			return fmt.Errorf("ordinary field %s.%s uses json:\",%s\"; use polytype.Optional[T] with json:\",omitzero\" to state omission explicitly at %s", owner.Name(), name, option, field.Position())
		}
	}
	if _, ok := field.Type().(*dst.StarExpr); ok {
		return fmt.Errorf("bare pointer field %s.%s is not admitted; use polytype.Nullable[T] to state nullability explicitly at %s", owner.Name(), name, field.Position())
	}
	return nil
}

func (l *typeGrammarLowerer) union(field registeredInterfaceField) (typegrammar.Union, error) {
	discriminator := field.DiscPropName
	if discriminator == "" {
		discriminator = l.builder.DiscriminatorProp
	}
	if discriminator == "" {
		discriminator = DefaultDiscriminatorPropName
	}
	union := typegrammar.Union{
		Interface:     typegrammar.Name{PackagePath: field.Interface.TypeSpec.Pkg().PkgPath, Name: field.Interface.TypeSpec.Name()},
		Discriminator: discriminator,
		Source:        field.Interface.TypeSpec.Position(),
	}
	for _, impl := range field.Interface.Impls {
		name := typegrammar.Name{PackagePath: impl.PkgPath, Name: impl.TypeName}
		if err := l.named(name); err != nil {
			return typegrammar.Union{}, fmt.Errorf("union implementation %s: %w", name, err)
		}
		position, _ := l.builder.find(syntax.TypeID{PkgPath: impl.PkgPath, TypeName: impl.TypeName})
		union.Variants = append(union.Variants, typegrammar.Variant{
			Implementation: name,
			Pointer:        impl.Indirection == syntax.Pointer,
			Tag:            field.Interface.DiscriminatorValue(impl),
			Source:         position,
		})
	}
	return union, nil
}

func (l *typeGrammarLowerer) enumConfig(owner syntax.StructType, field syntax.StructField) (enumFieldConfig, bool) {
	configs := l.builder.EnumV1[owner.Name()]
	for _, name := range field.Field.Names {
		if config, ok := configs[name.Name]; ok {
			return config, true
		}
	}
	return enumFieldConfig{}, false
}

func (l *typeGrammarLowerer) enum(set *syntax.EnumSet, mode typegrammar.EnumMode) (typegrammar.Type, error) {
	if len(set.Values) == 0 {
		return nil, fmt.Errorf("enum %s at %s has no constants of its exact named type", set.TypeSpec.Name(), set.TypeSpec.Position())
	}
	kind, err := enumScalarKind(set)
	if err != nil {
		return nil, err
	}
	node := &typegrammar.Enum{
		GoType: typegrammar.Name{PackagePath: set.TypeSpec.Pkg().PkgPath, Name: set.TypeSpec.Name()},
		Kind:   kind,
		Mode:   mode,
	}
	for _, member := range set.Values {
		node.Members = append(node.Members, typegrammar.EnumMember{Name: member.Name, Value: member.Value, Description: member.Description})
	}
	return node, nil
}

func (l *typeGrammarLowerer) recordEmbeddedDependency(expr syntax.TypeExpr) error {
	switch node := expr.Excerpt.(type) {
	case *dst.StarExpr:
		return l.recordEmbeddedDependency(expr.Derive(node.X))
	case *dst.ParenExpr:
		return l.recordEmbeddedDependency(expr.Derive(node.X))
	case *dst.Ident:
		pkgPath := node.Path
		if pkgPath == "" {
			pkgPath = expr.Pkg().PkgPath
		}
		if _, scalar := scalarKind(node.Name); scalar && node.Path == "" {
			return nil
		}
		return l.named(typegrammar.Name{PackagePath: pkgPath, Name: node.Name})
	default:
		return nil
	}
}

func wrapFieldValue(wrapper syntax.WrapperKind, typ typegrammar.Type) (typegrammar.FieldValue, error) {
	switch wrapper {
	case syntax.WrapperNone:
		return &typegrammar.Required{Type: typ}, nil
	case syntax.WrapperOptional:
		return &typegrammar.Optional{Type: typ}, nil
	case syntax.WrapperNullable:
		return &typegrammar.Nullable{Type: typ}, nil
	default:
		return nil, fmt.Errorf("unsupported field wrapper %v", wrapper)
	}
}

func scalarKind(name string) (typegrammar.ScalarKind, bool) {
	switch name {
	case "bool":
		return typegrammar.Bool, true
	case "string":
		return typegrammar.String, true
	case "int":
		return typegrammar.Int, true
	case "int8":
		return typegrammar.Int8, true
	case "int16":
		return typegrammar.Int16, true
	case "int32", "rune":
		return typegrammar.Int32, true
	case "int64":
		return typegrammar.Int64, true
	case "uint":
		return typegrammar.Uint, true
	case "uint8", "byte":
		return typegrammar.Uint8, true
	case "uint16":
		return typegrammar.Uint16, true
	case "uint32":
		return typegrammar.Uint32, true
	case "uint64":
		return typegrammar.Uint64, true
	case "float32":
		return typegrammar.Float32, true
	case "float64":
		return typegrammar.Float64, true
	default:
		return "", false
	}
}

func enumScalarKind(set *syntax.EnumSet) (typegrammar.ScalarKind, error) {
	obj := set.TypeSpec.Pkg().Types.Scope().Lookup(set.TypeSpec.Name())
	if obj == nil {
		return "", fmt.Errorf("enum type %s was not resolved by go/types", set.TypeSpec.Name())
	}
	basic, ok := obj.Type().Underlying().(*types.Basic)
	if !ok {
		return "", fmt.Errorf("enum type %s has unsupported underlying type %s", set.TypeSpec.Name(), obj.Type().Underlying())
	}
	kind, ok := scalarKind(basic.Name())
	if !ok || kind != typegrammar.String && !isIntegerScalar(kind) {
		return "", fmt.Errorf("enum type %s has unsupported underlying kind %s", set.TypeSpec.Name(), basic.Name())
	}
	return kind, nil
}

func isIntegerScalar(kind typegrammar.ScalarKind) bool {
	return kind == typegrammar.Int || kind == typegrammar.Int8 || kind == typegrammar.Int16 || kind == typegrammar.Int32 || kind == typegrammar.Int64 ||
		kind == typegrammar.Uint || kind == typegrammar.Uint8 || kind == typegrammar.Uint16 || kind == typegrammar.Uint32 || kind == typegrammar.Uint64
}

func arrayLength(expr syntax.TypeExpr, array *dst.ArrayType) (int64, error) {
	astNode := expr.Pkg().Decorator.Ast.Nodes[array]
	astExpr, ok := astNode.(ast.Expr)
	if !ok {
		return 0, fmt.Errorf("could not map array type to go/ast at %s", expr.Position())
	}
	typ := expr.Pkg().TypesInfo.TypeOf(astExpr)
	arrayType, ok := typ.(*types.Array)
	if !ok {
		return 0, fmt.Errorf("could not resolve fixed array length at %s", expr.Position())
	}
	return arrayType.Len(), nil
}

func rejectCustomWireType(scan syntax.ScanResult, name, position string) error {
	obj := scan.Pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil
	}
	for _, candidate := range []types.Type{obj.Type(), types.NewPointer(obj.Type())} {
		methods := types.NewMethodSet(candidate)
		for method := range methods.Methods() {
			method := method.Obj()
			if customWireMethod(method.Name(), method.Type()) {
				return fmt.Errorf("type %s.%s at %s defines %s; custom JSON/text wire mappings are not statically derivable", scan.Pkg.PkgPath, name, position, method.Name())
			}
		}
	}
	return nil
}

func customWireMethod(name string, typ types.Type) bool {
	sig, ok := typ.(*types.Signature)
	if !ok {
		return false
	}
	switch name {
	case "MarshalJSON", "MarshalText":
		return sig.Params().Len() == 0 && sig.Results().Len() == 2 && isByteSlice(sig.Results().At(0).Type()) && isError(sig.Results().At(1).Type())
	case "UnmarshalJSON", "UnmarshalText":
		return sig.Params().Len() == 1 && isByteSlice(sig.Params().At(0).Type()) && sig.Results().Len() == 1 && isError(sig.Results().At(0).Type())
	default:
		return false
	}
}

func isByteSlice(typ types.Type) bool {
	slice, ok := typ.(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := slice.Elem().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Byte
}

func isError(typ types.Type) bool { return types.Identical(typ, types.Universe.Lookup("error").Type()) }

func fieldName(field syntax.StructField) string {
	return strings.Join(goFieldNames(field), ",")
}

func goFieldNames(field syntax.StructField) []string {
	names := make([]string, 0, len(field.Field.Names))
	for _, name := range field.Field.Names {
		names = append(names, name.Name)
	}
	return names
}

type resolvedFieldName struct {
	goName string
	json   string
}

func resolvedFieldNames(field syntax.StructField) []resolvedFieldName {
	if len(field.Field.Names) == 1 {
		name := field.Field.Names[0].Name
		if !token.IsExported(name) {
			return nil
		}
		jsonName := name
		if tag := field.JSONTag(); tag != nil && tag.Options[0] != "" {
			jsonName = tag.Options[0]
		}
		return []resolvedFieldName{{goName: name, json: jsonName}}
	}
	result := make([]resolvedFieldName, 0, len(field.Field.Names))
	for _, name := range field.Field.Names {
		if token.IsExported(name.Name) {
			result = append(result, resolvedFieldName{goName: name.Name, json: name.Name})
		}
	}
	return result
}

func hasExplicitJSONName(field syntax.StructField) bool {
	tag := field.JSONTag()
	return tag != nil && len(tag.Options) > 0 && tag.Options[0] != ""
}

func embeddedFieldName(field syntax.StructField) (string, bool) {
	expr := field.Type()
	for {
		switch node := expr.(type) {
		case *dst.StarExpr:
			expr = node.X
		case *dst.ParenExpr:
			expr = node.X
		case *dst.Ident:
			return node.Name, true
		case *dst.SelectorExpr:
			return node.Sel.Name, true
		default:
			return "", false
		}
	}
}

func typeGrammarPackageError(scan syntax.ScanResult) error {
	if len(scan.Pkg.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("package %s has type-check errors: %s", scan.Pkg.PkgPath, scan.Pkg.Errors[0])
}
