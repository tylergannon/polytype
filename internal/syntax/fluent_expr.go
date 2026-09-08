package syntax

import (
	"fmt"
	"strconv"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// fluentChainLink represents one chained method call on top of a
// polytype.Declare(...) marker call, e.g. the `.Accessor(field, provider)`
// in `polytype.Declare(Example.Schema).Accessor(field, provider)`.
type fluentChainLink struct {
	methodName string
	call       *dst.CallExpr
}

// fluentMethodToOptionKind maps a supported *Declaration[T] chain method
// name to the SchemaMethodOptionKind produced by its legacy WithXxx
// equivalent, for the subset of chain methods that carry a field name (and,
// for Accessor/Method/Function, a provider reference).
var fluentMethodToOptionKind = map[string]SchemaMethodOptionKind{
	"Accessor":     SchemaMethodOptionKind("WithStructAccessorMethod"),
	"Method":       SchemaMethodOptionKind("WithStructFunctionMethod"),
	"Function":     SchemaMethodOptionKind("WithFunction"),
	"StringerEnum": SchemaMethodOptionKind("WithStringerEnum"),
}

// parseFluentChain walks a call expression down through any chained
// SelectorExpr/CallExpr links (e.g. `.Accessor(...).Method(...)`) looking
// for a base call that identifies as polytype.Declare(...). It returns the
// base Declare(...) call, the chain links in source (left-to-right,
// outermost-last) order, and whether a Declare(...)-rooted chain was found
// at all. A false result means outer is not a fluent jsonschema
// registration (e.g. some unrelated chained call in the same var block).
func parseFluentChain(outer CallExpr) (CallExpr, []fluentChainLink, bool) {
	var (
		links   []fluentChainLink
		current = outer
	)
	for {
		// The decorator collapses a package-qualified call target (e.g. the
		// base polytype.Declare(...) call) into a plain *dst.Ident rather
		// than a *dst.SelectorExpr, the same way it does for method-
		// expression receivers (see unwrapSchemaMethodReceiver). So check
		// for the Declare(...) base before assuming Fun is a chain-link
		// selector.
		if id, ok := current.IdentifyFunc(); ok && id.PkgPath == SchemaPackagePath && id.TypeName == MarkerFuncDeclare {
			for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
				links[i], links[j] = links[j], links[i]
			}
			return current, links, true
		}
		sel, ok := current.Concrete.Fun.(*dst.SelectorExpr)
		if !ok {
			return CallExpr{}, nil, false
		}
		innerCall, ok := sel.X.(*dst.CallExpr)
		if !ok {
			return CallExpr{}, nil, false
		}
		links = append(links, fluentChainLink{methodName: sel.Sel.Name, call: current.Concrete})
		current = NewCallExpr(innerCall, current.pkg, current.file)
	}
}

// ParseFluentDeclaration resolves a polytype.Declare(...) fluent
// registration (optionally chained with supported option methods) into the
// same SchemaMethod shape produced by the legacy NewJSONSchemaMethod /
// NewJSONSchemaFunc marker calls, so every downstream consumer (builder,
// codegen, TypeScript, codecs) can keep treating both forms identically.
// isMethodRoot reports whether fn was a method expression (append to
// ScanResult.SchemaMethods) or a free function (append to
// ScanResult.SchemaFuncs), mirroring the legacy dispatch in
// loadPackageInternal.
func (m MarkerFunctionCall) ParseFluentDeclaration(localFuncs []FuncDecl) (method SchemaMethod, isMethodRoot bool, err error) {
	funcArgs := m.CallExpr.Args()
	if len(funcArgs) != 1 {
		return SchemaMethod{}, false, fmt.Errorf("polytype.Declare expects exactly one argument (the schema func), at %s", m.CallExpr.Position())
	}

	var receiver TypeID
	var schemaMethodName string

	switch expr := funcArgs[0].Expr().(type) {
	case *dst.SelectorExpr:
		receiver, err = unwrapSchemaMethodReceiver(NewExpr(expr.X, m.CallExpr.pkg, m.CallExpr.file))
		if err != nil {
			return SchemaMethod{}, false, err
		}
		schemaMethodName = expr.Sel.Name
		isMethodRoot = true
	case *dst.Ident:
		fn, ok := findLocalFuncDecl(localFuncs, expr.Name)
		if !ok {
			return SchemaMethod{}, false, fmt.Errorf(
				"polytype.Declare: could not resolve free function %q at %s; it must be declared in this package (use a method expression or NewJSONSchemaFunc for other cases)",
				expr.Name, m.CallExpr.Position(),
			)
		}
		receiver, err = freeFuncReceiver(fn, m.CallExpr.pkg, m.CallExpr.file)
		if err != nil {
			return SchemaMethod{}, false, err
		}
		schemaMethodName = expr.Name
		isMethodRoot = false
	default:
		return SchemaMethod{}, false, fmt.Errorf("polytype.Declare: unsupported schema func expression (%T) at %s", expr, m.CallExpr.Position())
	}

	opts, err := parseFluentChainOptions(m.fluentLinks, receiver, m)
	if err != nil {
		return SchemaMethod{}, false, err
	}

	return SchemaMethod{
		Receiver:         receiver,
		SchemaMethodName: schemaMethodName,
		MarkerCall:       m,
		Options:          opts,
	}, isMethodRoot, nil
}

func findLocalFuncDecl(funcDecls []FuncDecl, name string) (*dst.FuncDecl, bool) {
	for _, fd := range funcDecls {
		if fd.Concrete.Name.Name == name {
			return fd.Concrete, true
		}
	}
	return nil, false
}

// freeFuncReceiver extracts T from a free function's sole parameter, for a
// polytype.Declare(fn) call where fn is a bare identifier (as opposed to a
// method expression, whose receiver is read directly off the AST by
// unwrapSchemaMethodReceiver). Declare[T any](fn func(T) json.RawMessage)
// guarantees fn has exactly one parameter, so this only fails for a
// malformed declaration (empty parameter list).
func freeFuncReceiver(fn *dst.FuncDecl, pkg *decorator.Package, file *dst.File) (TypeID, error) {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return TypeID{}, fmt.Errorf("polytype.Declare: free function %s must take exactly one parameter (the receiver type)", fn.Name.Name)
	}
	return parseFuncFromExpr(NewExpr(fn.Type.Params.List[0].Type, pkg, file)), nil
}

// fieldNameForReceiver extracts the field name from a
// `polytype.Field[Owner, Value]("FieldX")` reference or the legacy
// `exampleStruct{}.FieldX` selector expression, verifying that its owner
// matches receiver. Returns ok=false for other shapes or owners.
func fieldNameForReceiver(arg dst.Expr, receiver TypeID) (string, bool) {
	if call, ok := arg.(*dst.CallExpr); ok {
		var (
			fieldFuncExpr dst.Expr
			ownerExpr     dst.Expr
		)
		switch index := call.Fun.(type) {
		case *dst.IndexExpr:
			fieldFuncExpr = index.X
			ownerExpr = index.Index
		case *dst.IndexListExpr:
			if len(index.Indices) != 2 {
				return "", false
			}
			fieldFuncExpr = index.X
			ownerExpr = index.Indices[0]
		default:
			return "", false
		}
		if len(call.Args) != 1 {
			return "", false
		}
		fieldFunc, ok := fieldFuncExpr.(*dst.Ident)
		if !ok || fieldFunc.Name != "Field" || fieldFunc.Path != SchemaPackagePath {
			return "", false
		}
		owner, ok := ownerExpr.(*dst.Ident)
		if !ok || owner.Name != receiver.TypeName {
			return "", false
		}
		literal, ok := call.Args[0].(*dst.BasicLit)
		if !ok {
			return "", false
		}
		name, err := strconv.Unquote(literal.Value)
		return name, err == nil && name != ""
	}
	fieldSel, ok := arg.(*dst.SelectorExpr)
	if !ok {
		return "", false
	}
	lit, ok := fieldSel.X.(*dst.CompositeLit)
	if !ok {
		return "", false
	}
	recvIdent, ok := lit.Type.(*dst.Ident)
	if !ok || recvIdent.Name != receiver.TypeName {
		return "", false
	}
	return fieldSel.Sel.Name, true
}

// providerRef extracts a provider reference (either ReceiverType.MethodName,
// (ReceiverType).MethodName, or a bare free-function identifier) from a
// WithFunction/WithStructAccessorMethod/WithStructFunctionMethod-style
// second argument. matched=false mirrors the legacy generic scanner's
// silent skip of an option whose provider selector names some other
// receiver type. isMethod distinguishes a method expression from a free
// function; Go's type system cannot reject a free function passed where a
// method expression is expected (or the reverse), because both shapes
// satisfy the same func(T) json.Marshaler/func(F) json.Marshaler parameter
// type. The fluent chain-option parser (parseFluentChainOptions) checks
// isMethod against what each chain method requires and fails with a
// source-positioned error on a mismatch instead of silently producing a
// FieldProvider the code-generation template has no branch for.
func providerRef(provExpr dst.Expr, receiver TypeID) (name string, isMethod bool, matched bool) {
	switch p := provExpr.(type) {
	case *dst.SelectorExpr:
		switch x := p.X.(type) {
		case *dst.Ident:
			if x.Name != receiver.TypeName {
				return "", false, false
			}
			return p.Sel.Name, true, true
		case *dst.ParenExpr:
			switch inner := x.X.(type) {
			case *dst.Ident:
				if inner.Name == receiver.TypeName {
					return p.Sel.Name, true, true
				}
			case *dst.StarExpr:
				if id, ok := inner.X.(*dst.Ident); ok && id.Name == receiver.TypeName {
					return p.Sel.Name, true, true
				}
			}
			return "", false, false
		default:
			return "", false, false
		}
	case *dst.Ident:
		return p.Name, false, true
	}
	return "", false, true
}

// parseInterfaceNestedOptions parses the Discriminator(...)/Impl(...)
// options nested inside a WithInterface(field, ...)/.Interface(field, ...)
// call, shared verbatim between the legacy and fluent registration forms.
// parseFluentChainOptions converts the chained method calls following a
// polytype.Declare(...) base call into the same []SchemaMethodOptionInfo
// shape the legacy WithXxx(...) option list produces. Every method name not
// recognized here is a scanner error rather than a silent skip: unlike the
// legacy option list (which tolerates unrelated call expressions mixed into
// a variadic slice), every link in a fluent chain is a call this package
// itself parsed as being rooted at Declare(...), so an unrecognized link
// name only happens for a supported chain the scanner doesn't yet handle -
// exactly the case that must not fail silently.
func parseFluentChainOptions(links []fluentChainLink, receiver TypeID, m MarkerFunctionCall) ([]SchemaMethodOptionInfo, error) {
	var out []SchemaMethodOptionInfo
	for _, link := range links {
		ceArgs := link.call.Args
		var pos Expr = NewCallExpr(link.call, m.CallExpr.pkg, m.CallExpr.file)

		switch link.methodName {
		case "RenderProviders":
			out = append(out, SchemaMethodOptionInfo{Kind: SchemaMethodOptionKind("WithRenderProviders")})
		case "Ref":
			out = append(out, SchemaMethodOptionInfo{Kind: SchemaMethodOptionKind("AsRef")})
		case "StringerEnum":
			if len(ceArgs) != 1 {
				return nil, fmt.Errorf("polytype.Declare: .%s expects one field argument at %s", link.methodName, pos.Position())
			}
			fieldName, ok := fieldNameForReceiver(ceArgs[0], receiver)
			if !ok {
				return nil, fmt.Errorf("polytype.Declare: .%s expects a field selector on %s{} at %s", link.methodName, receiver.TypeName, pos.Position())
			}
			out = append(out, SchemaMethodOptionInfo{Kind: fluentMethodToOptionKind[link.methodName], FieldName: fieldName})
		case "Accessor", "Method", "Function":
			if len(ceArgs) != 2 {
				return nil, fmt.Errorf("polytype.Declare: .%s expects a field and provider argument at %s", link.methodName, pos.Position())
			}
			fieldName, ok := fieldNameForReceiver(ceArgs[0], receiver)
			if !ok {
				return nil, fmt.Errorf("polytype.Declare: .%s expects a field selector on %s{} at %s", link.methodName, receiver.TypeName, pos.Position())
			}
			providerName, providerIsMethod, matched := providerRef(ceArgs[1], receiver)
			if !matched {
				// providerRef only reports matched=false for a method-
				// expression selector whose receiver isn't the field's own
				// provider type (e.g. Function(fieldOfOtherType,
				// OtherType.Method) where OtherType isn't recognized as
				// this chain's receiver). The legacy variadic option list
				// tolerates this as an unrelated entry, but every fluent
				// chain link belongs to exactly one Declare[T] root, so this
				// can only be a mismatched provider, not an unrelated call;
				// silently dropping it would reproduce the same
				// silently-generated-panic gap as the isMethod mismatch
				// below.
				return nil, fmt.Errorf("polytype.Declare: .%s provider is not a supported method expression or free function at %s", link.methodName, pos.Position())
			}
			// Accessor/Method require a receiver method expression; Function
			// requires a free function. Go's type system accepts either
			// shape for all three (see providerRef's doc comment), so this
			// is the only place that can catch the mismatch.
			if wantMethod := link.methodName != "Function"; providerIsMethod != wantMethod {
				if wantMethod {
					return nil, fmt.Errorf("polytype.Declare: .%s provider must be a %s method expression, not a free function, at %s", link.methodName, receiver.TypeName, pos.Position())
				}
				return nil, fmt.Errorf("polytype.Declare: .Function provider must be a free function, not a %s method expression, at %s", receiver.TypeName, pos.Position())
			}
			out = append(out, SchemaMethodOptionInfo{
				Kind:             fluentMethodToOptionKind[link.methodName],
				FieldName:        fieldName,
				ProviderName:     providerName,
				ProviderIsMethod: providerIsMethod,
			})
		default:
			return nil, fmt.Errorf("polytype.Declare: unsupported chain method %q at %s", link.methodName, pos.Position())
		}
	}
	return out, nil
}
