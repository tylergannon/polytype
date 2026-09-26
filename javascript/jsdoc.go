package javascript

import (
	"fmt"
	"strings"
	"unicode"
)

type typeExpr interface {
	typeExpression()
}

type keywordType string
type literalType string
type referenceType string

type objectType struct {
	properties []property
}

type property struct {
	name        string
	description string
	optional    bool
	typeExpr    typeExpr
}

type genericType struct {
	name      string
	arguments []typeExpr
}

type unionType struct {
	members []typeExpr
}

type intersectionType struct {
	members []typeExpr
}

func (keywordType) typeExpression()      {}
func (literalType) typeExpression()      {}
func (referenceType) typeExpression()    {}
func (objectType) typeExpression()       {}
func (genericType) typeExpression()      {}
func (unionType) typeExpression()        {}
func (intersectionType) typeExpression() {}

func union(members ...typeExpr) typeExpr {
	flat := make([]typeExpr, 0, len(members))
	for _, member := range members {
		if nested, ok := member.(unionType); ok {
			flat = append(flat, nested.members...)
		} else {
			flat = append(flat, member)
		}
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return unionType{members: flat}
}

func intersection(members ...typeExpr) typeExpr {
	flat := make([]typeExpr, 0, len(members))
	for _, member := range members {
		if nested, ok := member.(intersectionType); ok {
			flat = append(flat, nested.members...)
		} else {
			flat = append(flat, member)
		}
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return intersectionType{members: flat}
}

type typedef struct {
	name        string
	description string
	body        typeExpr
}

const (
	precedenceUnion = iota + 1
	precedenceIntersection
	precedencePrimary
)

func printModule(typedefs []typedef) []byte {
	var out strings.Builder
	out.WriteString(GeneratedHeader)
	if len(typedefs) == 0 {
		// A JSDoc-only module has no runtime bindings; the empty export keeps
		// it a valid ES module whose typedefs are reachable with
		// `import('./types.js').Name`.
		out.WriteString("export {};\n")
		return []byte(out.String())
	}
	out.WriteByte('\n')
	for i, doc := range typedefs {
		writeTypedef(&out, doc)
		if i != len(typedefs)-1 {
			out.WriteByte('\n')
		}
	}
	out.WriteByte('\n')
	out.WriteString("export {};\n")
	return []byte(out.String())
}

func writeTypedef(out *strings.Builder, doc typedef) {
	out.WriteString("/**\n")
	writeCommentLines(out, doc.description)
	if body, ok := doc.body.(objectType); ok && jsdocPropertyNamesSafe(body.properties) {
		out.WriteString(" * @typedef {Object} ")
		out.WriteString(doc.name)
		out.WriteByte('\n')
		for _, prop := range body.properties {
			out.WriteString(" * @property {")
			writeType(out, prop.typeExpr, 0)
			out.WriteString("} ")
			writePropertyName(out, prop.name, prop.optional)
			writeDescriptionSuffix(out, prop.description)
			out.WriteByte('\n')
		}
	} else {
		out.WriteString(" * @typedef {")
		writeType(out, doc.body, 0)
		out.WriteString("} ")
		out.WriteString(doc.name)
		out.WriteByte('\n')
	}
	out.WriteString(" */\n")
}

// jsdocPropertyNamesSafe reports whether every property can be named by a
// JSDoc @property tag. The TypeScript JSDoc parser accepts identifier-like
// names and hyphenated namepaths, but rejects quoted, spaced, bracketed, or
// punctuated names. Objects with such keys fall back to a single inline object
// type, which quotes every key and keeps the module type-correct even though
// per-property JSDoc is not representable.
func jsdocPropertyNamesSafe(properties []property) bool {
	for _, prop := range properties {
		if !jsdocPropertyNameSafe(prop.name) {
			return false
		}
	}
	return true
}

func jsdocPropertyNameSafe(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || r == '$':
		case r == '-' && i > 0:
		case unicode.IsLetter(r):
		case unicode.IsDigit(r) && i > 0:
		default:
			return false
		}
	}
	return true
}

func writePropertyName(out *strings.Builder, name string, optional bool) {
	if optional {
		out.WriteByte('[')
	}
	out.WriteString(name)
	if optional {
		out.WriteByte(']')
	}
}

// writeCommentLines writes a description as JSDoc body lines, sanitizing
// escapes exactly as the TypeScript backend does.
func writeCommentLines(out *strings.Builder, description string) {
	if description == "" {
		return
	}
	for line := range strings.SplitSeq(sanitizeComment(description), "\n") {
		out.WriteString(" *")
		if line != "" {
			out.WriteByte(' ')
			out.WriteString(line)
		}
		out.WriteByte('\n')
	}
}

// writeDescriptionSuffix writes a property description after the property
// name, continuing long descriptions on their own comment lines.
func writeDescriptionSuffix(out *strings.Builder, description string) {
	if description == "" {
		return
	}
	lines := strings.Split(sanitizeComment(description), "\n")
	if lines[0] != "" {
		out.WriteByte(' ')
		out.WriteString(lines[0])
	}
	for _, line := range lines[1:] {
		out.WriteString("\n *")
		if line != "" {
			out.WriteByte(' ')
			out.WriteString(line)
		}
	}
}

func precedence(expr typeExpr) int {
	switch expr.(type) {
	case unionType:
		return precedenceUnion
	case intersectionType:
		return precedenceIntersection
	default:
		return precedencePrimary
	}
}

func writeType(out *strings.Builder, expr typeExpr, parentPrecedence int) {
	currentPrecedence := precedence(expr)
	parenthesize := currentPrecedence < parentPrecedence
	if parenthesize {
		out.WriteByte('(')
	}
	switch n := expr.(type) {
	case keywordType:
		out.WriteString(string(n))
	case literalType:
		out.WriteString(string(n))
	case referenceType:
		out.WriteString(string(n))
	case objectType:
		writeObject(out, n)
	case genericType:
		out.WriteString(n.name)
		out.WriteByte('<')
		for i, argument := range n.arguments {
			if i > 0 {
				out.WriteString(", ")
			}
			writeType(out, argument, 0)
		}
		out.WriteByte('>')
	case unionType:
		for i, member := range n.members {
			if i > 0 {
				out.WriteString(" | ")
			}
			writeType(out, member, precedenceUnion)
		}
	case intersectionType:
		for i, member := range n.members {
			if i > 0 {
				out.WriteString(" & ")
			}
			writeType(out, member, precedenceIntersection)
		}
	default:
		panic(fmt.Sprintf("unknown JavaScript type expression %T", expr))
	}
	if parenthesize {
		out.WriteByte(')')
	}
}

func writeObject(out *strings.Builder, object objectType) {
	out.WriteByte('{')
	for i, prop := range object.properties {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(quote(prop.name))
		if prop.optional {
			out.WriteByte('?')
		}
		out.WriteString(": ")
		writeType(out, prop.typeExpr, 0)
	}
	out.WriteByte('}')
}

func sanitizeComment(comment string) string {
	comment = strings.ToValidUTF8(comment, "�")
	comment = strings.ReplaceAll(comment, "\r\n", "\n")
	comment = strings.ReplaceAll(comment, "\r", "\n")
	comment = strings.ReplaceAll(comment, "*/", "*\\/")
	var out strings.Builder
	for _, r := range comment {
		switch {
		case r == '\n' || r == '\t' || r >= ' ' && r != '\u2028' && r != '\u2029' && !unicode.IsControl(r):
			out.WriteRune(r)
		case r <= 0xffff:
			fmt.Fprintf(&out, "\\u%04X", r)
		default:
			fmt.Fprintf(&out, "\\u{%X}", r)
		}
	}
	return out.String()
}
