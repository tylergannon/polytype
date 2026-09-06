package codegen

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/typegrammar"
)

//go:embed codec.go.tmpl
var codecTemplate string

// Options configures the emitted file.
type Options struct {
	// PackageName is the package clause of the emitted file.
	PackageName string
	// ImportPath is the import path of the package the file will live in.
	// Types declared there are referenced unqualified, so the file never
	// imports itself.
	ImportPath string
}

// rootPackagePath names the roots for validation and diagnostics only. Roots
// are anonymous, so they need a synthetic identity that cannot collide with a
// real Go import path.
const rootPackagePath = "polytype.invalid/devalue/roots"

// Generate emits one Go source file holding, for each definition and each
// root, an encoder and a strict decoder as free functions over the type's
// exported fields, plus Stringify and Parse wrappers. It never mutates defs.
func Generate(defs typegrammar.Definitions, roots []typegrammar.Type, opts Options) ([]byte, error) {
	if opts.PackageName == "" {
		return nil, fmt.Errorf("generate devalue codecs: Options.PackageName is required")
	}
	for i, root := range roots {
		if root == nil {
			return nil, fmt.Errorf("generate devalue codecs: root %d is nil", i)
		}
	}
	if err := defs.ValidateWithRoots(roots); err != nil {
		return nil, fmt.Errorf("generate devalue codecs: %w", err)
	}

	g := &generator{
		opts:    opts,
		defs:    defs,
		index:   make(map[typegrammar.Name]typegrammar.Definition, len(defs)),
		aliases: make(map[string]string),
		used:    make(map[string]bool, len(reservedIdentifiers)),
	}
	for _, name := range reservedIdentifiers {
		g.used[name] = true
	}
	for _, def := range defs {
		g.index[def.Name] = def
	}
	g.allocateNames(roots)

	var functions []string
	for _, def := range defs {
		encoded, err := g.definitionFunctions(def)
		if err != nil {
			return nil, err
		}
		functions = append(functions, encoded...)
	}
	for i, root := range roots {
		encoded, err := g.rootFunctions(i, root)
		if err != nil {
			return nil, err
		}
		functions = append(functions, encoded...)
	}

	source, err := builder.RenderTemplate(codecTemplate, struct {
		PackageName string
		Imports     []importSpec
		Functions   []string
	}{PackageName: opts.PackageName, Imports: g.imports(), Functions: functions})
	if err != nil {
		return nil, fmt.Errorf("generate devalue codecs: %w", err)
	}
	formatted, err := builder.FormatCodeWithGoimports(source.Bytes())
	if err != nil {
		return nil, fmt.Errorf("generate devalue codecs: %w", err)
	}
	return formatted, nil
}

type importSpec struct {
	Alias string
	Path  string
}

// reservedIdentifiers are the names the emitted prelude and its imports own.
// Generated function names and package aliases are allocated around them.
var reservedIdentifiers = []string{
	"fmt", "json", "math", "slices", "strconv", "time", "devalue",
	"dvAt", "dvErr", "dvKind", "dvBool", "dvString", "dvNumber", "dvFloat32",
	"dvInteger", "dvArray", "dvObject", "dvKnown", "dvRequired", "dvPresent",
	"dvWithout", "dvTagged", "dvEncodeTime", "dvDecodeTime",
}

const devaluePackagePath = "github.com/tylergannon/polytype/devalue"

type generator struct {
	opts  Options
	defs  typegrammar.Definitions
	index map[typegrammar.Name]typegrammar.Definition

	// bases maps a definition to its collision-free base identifier; roots use
	// rootBases by position. used holds every identifier already handed out.
	bases     map[typegrammar.Name]string
	rootBases []string
	used      map[string]bool

	// aliases maps an import path to the identifier the file refers to it by.
	// The package the file lives in is never aliased and never imported.
	aliases map[string]string
}

func (g *generator) allocateNames(roots []typegrammar.Type) {
	counts := make(map[string]int, len(g.defs))
	bases := make(map[typegrammar.Name]string, len(g.defs))
	for _, def := range g.defs {
		base := exportedIdentifier(def.Name.Name)
		bases[def.Name] = base
		counts[base]++
	}
	g.bases = make(map[typegrammar.Name]string, len(g.defs))
	for _, def := range g.defs {
		base := bases[def.Name]
		if counts[base] > 1 {
			// Two packages declare the same local name. Qualify by the full
			// identity so the choice is stable across runs and machines.
			base += "_" + hex.EncodeToString([]byte(def.Name.PackagePath))
		}
		g.bases[def.Name] = base
		g.claim(base)
	}
	for i := range roots {
		base := fmt.Sprintf("Root%d", i)
		for g.used[base] {
			base += "_"
		}
		g.rootBases = append(g.rootBases, base)
		g.claim(base)
	}
}

func (g *generator) claim(base string) {
	g.used[base] = true
	for _, prefix := range []string{"Encode", "Decode", "Stringify", "Parse", "enc", "dec"} {
		g.used[prefix+base] = true
	}
}

// qualify renders name as a Go type expression, importing its package under a
// deterministic alias unless the file itself lives there.
func (g *generator) qualify(name typegrammar.Name) string {
	if name.PackagePath == g.opts.ImportPath {
		return name.Name
	}
	alias, ok := g.aliases[name.PackagePath]
	if !ok {
		alias = g.allocateAlias(name.PackagePath)
		g.aliases[name.PackagePath] = alias
	}
	return alias + "." + name.Name
}

func (g *generator) allocateAlias(importPath string) string {
	base := "pkg"
	if last := path.Base(importPath); last != "" {
		if candidate := safeIdentifier(last); candidate != "" {
			base = "pkg_" + candidate
		}
	}
	alias := base
	for i := 2; g.used[alias]; i++ {
		alias = base + strconv.Itoa(i)
	}
	g.used[alias] = true
	return alias
}

func (g *generator) imports() []importSpec {
	specs := []importSpec{
		{Alias: "", Path: "encoding/json"},
		{Alias: "", Path: "fmt"},
		{Alias: "", Path: "math"},
		{Alias: "", Path: "slices"},
		{Alias: "", Path: "strconv"},
		{Alias: "", Path: "time"},
		{Alias: "devalue", Path: devaluePackagePath},
	}
	paths := make([]string, 0, len(g.aliases))
	for importPath := range g.aliases {
		paths = append(paths, importPath)
	}
	slices.Sort(paths)
	for _, importPath := range paths {
		specs = append(specs, importSpec{Alias: g.aliases[importPath], Path: importPath})
	}
	return specs
}

// goType renders the structural Go type of a grammar node. A definition's own
// type is spelled by its name instead; see definitionFunctions.
func (g *generator) goType(t typegrammar.Type) (string, error) {
	switch n := t.(type) {
	case *typegrammar.Scalar:
		return string(n.Kind), nil
	case *typegrammar.Time:
		return "time.Time", nil
	case *typegrammar.Enum:
		return g.qualify(n.GoType), nil
	case *typegrammar.Ref:
		return g.qualify(n.Target), nil
	case *typegrammar.Pointer:
		element, err := g.goType(n.Element)
		return "*" + element, err
	case *typegrammar.Slice:
		element, err := g.goType(n.Element)
		return "[]" + element, err
	case *typegrammar.Array:
		element, err := g.goType(n.Element)
		return "[" + strconv.FormatInt(n.Length, 10) + "]" + element, err
	case *typegrammar.Object:
		return "", fmt.Errorf("generate devalue codecs: an anonymous struct type is supported only as a definition's own type")
	default:
		return "", fmt.Errorf("generate devalue codecs: unsupported type constructor %T", t)
	}
}

func (g *generator) definitionFunctions(def typegrammar.Definition) ([]string, error) {
	goType := g.qualify(def.Name)
	base := g.bases[def.Name]
	return g.functions(base, goType, def.Type, def.Name.String())
}

func (g *generator) rootFunctions(i int, root typegrammar.Type) ([]string, error) {
	goType, err := g.goType(root)
	if err != nil {
		return nil, err
	}
	return g.functions(g.rootBases[i], goType, root, fmt.Sprintf("%s.Root%d", rootPackagePath, i))
}

// functions emits the encoder, decoder and wrappers for one entry point. goType
// is the Go type the caller passes and receives; node is its grammar shape.
func (g *generator) functions(base, goType string, node typegrammar.Type, diagnostic string) ([]string, error) {
	encode, err := g.encodeFunction(base, goType, node, diagnostic)
	if err != nil {
		return nil, err
	}
	decode, err := g.decodeFunction(base, goType, node, diagnostic)
	if err != nil {
		return nil, err
	}
	wrappers := fmt.Sprintf(`// Encode%[1]s converts v into the devalue value model.
func Encode%[1]s(v %[2]s) (any, error) { return enc%[1]s(v, "") }

// Decode%[1]s converts a devalue value model tree into a %[2]s, rejecting any
// shape the type grammar does not admit.
func Decode%[1]s(raw any) (%[2]s, error) { return dec%[1]s(raw, "") }

// Stringify%[1]s encodes v and serializes it with devalue.
func Stringify%[1]s(v %[2]s) (string, error) {
	encoded, err := enc%[1]s(v, "")
	if err != nil {
		return "", err
	}
	return devalue.Stringify(encoded)
}

// Parse%[1]s parses a devalue document and decodes it into a %[2]s.
func Parse%[1]s(s string) (%[2]s, error) {
	var zero %[2]s
	parsed, err := devalue.Parse(s, nil)
	if err != nil {
		return zero, err
	}
	return dec%[1]s(parsed, "")
}`, base, goType)
	return []string{encode, decode, wrappers}, nil
}

func (g *generator) encodeFunction(base, goType string, node typegrammar.Type, diagnostic string) (string, error) {
	e := &emitter{g: g, diagnostic: diagnostic}
	source := "v"
	if _, isObject := node.(*typegrammar.Object); !isObject {
		structural, err := g.goType(node)
		if err != nil {
			return "", err
		}
		if structural != goType {
			source = e.name("src")
			e.writef("%s := %s(v)", source, structural)
		}
	}
	result, err := e.encode(node, source, "at")
	if err != nil {
		return "", err
	}
	e.writef("return %s, nil", result)
	return fmt.Sprintf("func enc%s(v %s, at string) (any, error) {\n%s}", base, goType, e.b.String()), nil
}

func (g *generator) decodeFunction(base, goType string, node typegrammar.Type, diagnostic string) (string, error) {
	e := &emitter{g: g, diagnostic: diagnostic, zero: "dvZero"}
	e.writef("var dvZero %s", goType)
	result, err := e.decodeAs(node, goType, "raw", "at")
	if err != nil {
		return "", err
	}
	e.writef("return %s, nil", result)
	return fmt.Sprintf("func dec%s(raw any, at string) (%s, error) {\n%s}", base, goType, e.b.String()), nil
}

// emitter accumulates the statements of one generated function. Names are
// numbered per function, so nesting never shadows an outer binding.
type emitter struct {
	g          *generator
	b          strings.Builder
	n          int
	zero       string // return expression for a decoder's error paths
	diagnostic string // the grammar location, for generator-side errors
}

func (e *emitter) name(prefix string) string {
	e.n++
	return prefix + strconv.Itoa(e.n)
}

func (e *emitter) writef(format string, args ...any) {
	fmt.Fprintf(&e.b, format+"\n", args...)
}

// check emits the error propagation for a call that has just bound err.
func (e *emitter) check() {
	if e.zero == "" {
		e.writef("if err != nil { return nil, err }")
		return
	}
	e.writef("if err != nil { return %s, err }", e.zero)
}

// fail emits a return of a dvErr built from the given format and arguments.
func (e *emitter) fail(at, format string, args ...string) {
	var call strings.Builder
	fmt.Fprintf(&call, "dvErr(%s, %s", at, strconv.Quote(format))
	for _, arg := range args {
		call.WriteString(", " + arg)
	}
	call.WriteString(")")
	if e.zero == "" {
		e.writef("return nil, %s", call.String())
		return
	}
	e.writef("return %s, %s", e.zero, call.String())
}

func (e *emitter) errorf(format string, args ...any) error {
	return fmt.Errorf("generate devalue codecs: %s: %s", e.diagnostic, fmt.Sprintf(format, args...))
}

// childPath returns the Go expression for a named property's path. It is an
// expression, not a binding, so a node that never reports a diagnostic does
// not leave an unused variable behind.
func (e *emitter) childPath(at, segment string) string {
	return at + " + " + strconv.Quote("/"+escapePointer(segment))
}

// branch returns an emitter that continues this one's name numbering but
// accumulates into its own buffer, so a loop body can be inspected before its
// header is written.
func (e *emitter) branch() *emitter {
	return &emitter{g: e.g, n: e.n, zero: e.zero, diagnostic: e.diagnostic}
}

// loop writes a range loop over src whose body is sub. The index binding is
// dropped when the body never mentions it, which is the common case for
// collections of scalars.
func (e *emitter) loop(sub *emitter, index, item, src string) {
	body := sub.b.String()
	name := index
	if !regexp.MustCompile(`\b` + regexp.QuoteMeta(index) + `\b`).MatchString(body) {
		name = "_"
	}
	e.writef("for %s, %s := range %s {", name, item, src)
	e.b.WriteString(body)
	e.writef("}")
	e.n = sub.n
}

// indexPath returns the Go expression for the path of the element at the
// position held by the index variable.
func (e *emitter) indexPath(at, index string) string {
	return at + ` + "/" + strconv.Itoa(` + index + ")"
}
