package builder

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/dave/dst"
	"github.com/tylergannon/polytype/internal/common"
	"github.com/tylergannon/polytype/internal/syntax"
	"github.com/tylergannon/polytype/typegrammar"
)

// TypeDefinitions lowers the builder's selected roots and their reachable
// named dependencies into the validated, static type grammar. It performs no
// rendering and does not mutate builder state or output files.
func (s *SchemaBuilder) TypeDefinitions() (typegrammar.Definitions, error) {
	if s == nil {
		return nil, fmt.Errorf("build type definitions: nil SchemaBuilder")
	}
	if err := typeGrammarPackageError(s.Scan); err != nil {
		return nil, err
	}
	l := typeGrammarLowerer{
		builder: s,
		index:   make(map[typegrammar.Name]int),
	}
	for _, method := range s.SchemaMethods() {
		name := typegrammar.Name{PackagePath: method.Receiver.PkgPath, Name: method.Receiver.TypeName}
		if err := l.named(name); err != nil {
			return nil, fmt.Errorf("build type definitions for %s: %w", name, err)
		}
	}
	for _, fn := range s.SchemaFreeFuncs() {
		name := typegrammar.Name{PackagePath: fn.Receiver.PkgPath, Name: fn.Receiver.TypeName}
		if err := l.named(name); err != nil {
			return nil, fmt.Errorf("build type definitions for %s: %w", name, err)
		}
	}
	if err := l.defs.Validate(); err != nil {
		return nil, fmt.Errorf("validate type definitions: %w", err)
	}
	return l.defs, nil
}

type typeGrammarLowerer struct {
	builder *SchemaBuilder
	defs    typegrammar.Definitions
	index   map[typegrammar.Name]int
}

func (l *typeGrammarLowerer) named(name typegrammar.Name) error {
	if _, ok := l.index[name]; ok {
		return nil
	}
	scan, ok := l.builder.Scan.GetPackage(name.PackagePath)
	if !ok {
		return fmt.Errorf("unresolved package %q for named type %s", name.PackagePath, name)
	}
	if err := typeGrammarPackageError(scan); err != nil {
		return err
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
				return fmt.Errorf("registered interface %s is valid only as a configured direct field", name)
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
		return err
	}
	var (
		typ typegrammar.Type
		err error
	)
	if enumSet != nil {
		typ, err = l.enum(enumSet, typegrammar.EnumValues)
	} else {
		typ, err = l.definitionType(typeSpec)
	}
	if err != nil {
		return fmt.Errorf("type %s at %s: %w", name, typeSpec.Position(), err)
	}
	l.defs[idx].Type = typ
	return nil
}

func (l *typeGrammarLowerer) definitionType(typeSpec syntax.TypeSpec) (typegrammar.Type, error) {
	if object, ok := typeSpec.Type().Expr().(*dst.StructType); ok {
		fields, err := l.structFields(syntax.NewStructType(object, typeSpec), true)
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
		if _, ok := l.builder.Scan.GetPackage(pkgPath); !ok {
			return nil, fmt.Errorf("unresolved external type %s; no static wire shape was loaded", name)
		}
		if err := l.named(name); err != nil {
			return nil, err
		}
		return &typegrammar.Ref{Target: name}, nil

	case *dst.SelectorExpr:
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
		fields, err := l.structFields(owner, false)
		if err != nil {
			return nil, err
		}
		return &typegrammar.Object{Fields: fields}, nil

	case *dst.MapType:
		return nil, fmt.Errorf("maps are outside the static type grammar at %s", expr.Position())
	case *dst.ChanType:
		return nil, fmt.Errorf("channels are outside the static type grammar at %s", expr.Position())
	case *dst.FuncType:
		return nil, fmt.Errorf("functions are outside the static type grammar at %s", expr.Position())
	case *dst.InterfaceType:
		return nil, fmt.Errorf("interfaces are valid only as configured direct fields at %s", expr.Position())
	case *dst.IndexExpr, *dst.IndexListExpr:
		return nil, fmt.Errorf("presence wrappers are valid only as complete direct named fields at %s", expr.Position())
	default:
		return nil, fmt.Errorf("unsupported type expression %T at %s", node, expr.Position())
	}
}

func (l *typeGrammarLowerer) structFields(owner syntax.StructType, namedOwner bool) ([]typegrammar.Field, error) {
	var (
		candidates []typeGrammarFieldCandidate
		order      int
	)
	if err := l.collectStructFields(owner, namedOwner, 0, &order, &candidates); err != nil {
		return nil, err
	}
	winners := dominantFieldCandidates(candidates)
	fields := make([]typegrammar.Field, 0, len(winners))
	for _, candidate := range winners {
		value, err := l.fieldValue(candidate.owner, candidate.field, candidate.namedOwner)
		if err != nil {
			return nil, err
		}
		fields = append(fields, typegrammar.Field{
			GoName:      candidate.name.goName,
			JSONName:    candidate.name.json,
			Value:       value,
			Description: candidate.field.Comments(),
			Source:      candidate.field.Position(),
		})
	}
	return fields, nil
}

type typeGrammarFieldCandidate struct {
	owner      syntax.StructType
	field      syntax.StructField
	name       resolvedFieldName
	depth      int
	order      int
	tagged     bool
	namedOwner bool
}

func (l *typeGrammarLowerer) collectStructFields(owner syntax.StructType, namedOwner bool, depth int, order *int, candidates *[]typeGrammarFieldCandidate) error {
	for _, field := range owner.Fields() {
		if field.Embedded() && !hasExplicitJSONName(field) {
			if err := l.recordEmbeddedDependency(field.TypeExpr); err != nil {
				return fmt.Errorf("embedded field at %s: %w", field.Position(), err)
			}
			embedded, err := l.builder.resolveEmbeddedType(field.TypeExpr, nil)
			if err != nil {
				return err
			}
			if err := l.collectStructFields(embedded, true, depth+1, order, candidates); err != nil {
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
				owner: owner, field: field, name: name, depth: depth, order: *order,
				tagged: hasExplicitJSONName(field), namedOwner: namedOwner,
			})
			*order++
		}
	}
	return nil
}

func dominantFieldCandidates(candidates []typeGrammarFieldCandidate) []typeGrammarFieldCandidate {
	byName := make(map[string][]typeGrammarFieldCandidate)
	for _, candidate := range candidates {
		byName[candidate.name.json] = append(byName[candidate.name.json], candidate)
	}
	winners := make([]typeGrammarFieldCandidate, 0, len(byName))
	for _, group := range byName {
		minDepth := group[0].depth
		for _, candidate := range group[1:] {
			if candidate.depth < minDepth {
				minDepth = candidate.depth
			}
		}
		var shallow []typeGrammarFieldCandidate
		for _, candidate := range group {
			if candidate.depth == minDepth {
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
		}
	}
	slices.SortFunc(winners, func(a, b typeGrammarFieldCandidate) int { return a.order - b.order })
	return winners
}

func (l *typeGrammarLowerer) fieldValue(owner syntax.StructType, field syntax.StructField, namedOwner bool) (typegrammar.FieldValue, error) {
	if field.Field.Tag != nil {
		if common.ParseJSONSchemaTag(field.Field.Tag.Value).HasRef {
			return nil, fmt.Errorf("field %s.%s at %s uses an explicit schema ref with no resolved static type target", owner.Name(), fieldName(field), field.Position())
		}
	}
	if tag := field.JSONTag(); tag != nil && slices.Contains(tag.Options[1:], "string") {
		return nil, fmt.Errorf("field %s.%s at %s uses json:\",string\", whose wire mapping is outside the static type grammar", owner.Name(), fieldName(field), field.Position())
	}
	if providers := l.builder.TypeProvidersMap[owner.Name()]; hasProviderForGoField(providers, goFieldNames(field)) {
		return nil, fmt.Errorf("field %s.%s at %s uses a runtime schema provider with no statically resolved wire type", owner.Name(), fieldName(field), field.Position())
	}

	wrapper, inner, err := field.Wrapper()
	if err != nil {
		return nil, err
	}
	if wrapper == syntax.WrapperOptional && !field.HasJSONOption("omitzero") {
		return nil, fmt.Errorf("%s field %s.%s requires json:\",omitzero\" at %s", wrapper, owner.Name(), fieldName(field), field.Position())
	}

	if cfg, ok := l.enumConfig(owner, field); ok {
		if !namedOwner {
			return nil, fmt.Errorf("field-local enum registration on %s.%s requires a direct field of a named object", owner.Name(), fieldName(field))
		}
		interfaceField, unionErr := l.builder.resolveRegisteredInterfaceField(owner, field)
		if unionErr != nil {
			return nil, unionErr
		} else if interfaceField != nil {
			return nil, fmt.Errorf("field %s.%s cannot be both an enum and registered interface", owner.Name(), fieldName(field))
		}
		renderType := field.Type()
		if wrapper != syntax.WrapperNone {
			renderType = inner
		}
		ident, ok := renderType.(*dst.Ident)
		if !ok {
			return nil, fmt.Errorf("field %s.%s enum registration requires a direct named enum type at %s", owner.Name(), fieldName(field), field.Position())
		}
		enumSet, err := l.resolveFieldEnum(ident, field)
		if err != nil {
			return nil, err
		}
		if err := l.named(typegrammar.Name{PackagePath: enumSet.TypeSpec.Pkg().PkgPath, Name: enumSet.TypeSpec.Name()}); err != nil {
			return nil, err
		}
		mode := typegrammar.EnumValues
		kind, err := enumScalarKind(enumSet)
		if err != nil {
			return nil, err
		}
		if cfg.UseStringer && kind != typegrammar.String {
			mode = typegrammar.EnumNames
		}
		typ, err := l.enum(enumSet, mode)
		if err != nil {
			return nil, err
		}
		return wrapFieldValue(wrapper, typ)
	}

	if namedOwner {
		interfaceField, err := l.builder.resolveRegisteredInterfaceField(owner, field)
		if err != nil {
			return nil, err
		}
		if interfaceField != nil {
			union, err := l.union(*interfaceField)
			if err != nil {
				return nil, err
			}
			switch {
			case interfaceField.Repeated:
				return &typegrammar.UnionSlice{Union: union}, nil
			case interfaceField.Optional:
				return &typegrammar.OptionalUnion{Union: union}, nil
			default:
				return &union, nil
			}
		}
	}

	renderType := field.Type()
	if wrapper != syntax.WrapperNone {
		renderType = inner
	}
	typ, err := l.typ(field.Derive(renderType))
	if err != nil {
		return nil, fmt.Errorf("field %s.%s: %w", owner.Name(), fieldName(field), err)
	}
	return wrapFieldValue(wrapper, typ)
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
			Tag:            impl.TypeName,
			Source:         position,
		})
	}
	return union, nil
}

func (l *typeGrammarLowerer) enumConfig(owner syntax.StructType, field syntax.StructField) (struct{ UseStringer bool }, bool) {
	configs := l.builder.EnumV1[owner.Name()]
	for _, name := range field.Field.Names {
		if config, ok := configs[name.Name]; ok {
			return config, true
		}
	}
	return struct{ UseStringer bool }{}, false
}

func (l *typeGrammarLowerer) resolveFieldEnum(ident *dst.Ident, field syntax.StructField) (*syntax.EnumSet, error) {
	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = field.Pkg().PkgPath
	}
	scan, ok := l.builder.Scan.GetPackage(pkgPath)
	if !ok {
		return nil, fmt.Errorf("field %s: enum package %q was not loaded", fieldName(field), pkgPath)
	}
	enumSet := scan.Constants[ident.Name]
	if enumSet == nil {
		if typeSpec, found := scan.LocalNamedTypes[ident.Name]; found {
			var err error
			enumSet, err = syntax.ResolveEnum(typeSpec)
			if err != nil {
				return nil, err
			}
		}
	}
	if enumSet == nil || len(enumSet.Values) == 0 {
		return nil, fmt.Errorf("field %s: registered enum type %s.%s has no discoverable constants", fieldName(field), pkgPath, ident.Name)
	}
	return enumSet, nil
}

func (l *typeGrammarLowerer) enum(set *syntax.EnumSet, mode typegrammar.EnumMode) (typegrammar.Type, error) {
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
		node.Members = append(node.Members, typegrammar.EnumMember{Name: member.Name, Value: member.Value})
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
