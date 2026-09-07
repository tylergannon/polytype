package typescript

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	precedenceUnion = iota + 1
	precedenceIntersection
	precedencePrimary
)

func printModule(aliases []typeAlias) []byte {
	var out strings.Builder
	out.WriteString(GeneratedHeader)
	if len(aliases) == 0 {
		out.WriteString("export {};\n")
		return []byte(out.String())
	}
	out.WriteByte('\n')
	for i, alias := range aliases {
		writeDoc(&out, alias.description, "")
		out.WriteString("export type ")
		out.WriteString(alias.name)
		out.WriteString(" = ")
		writeType(&out, alias.typeExpr, 0, "")
		out.WriteString(";\n")
		if i != len(aliases)-1 {
			out.WriteByte('\n')
		}
	}
	return []byte(out.String())
}

func printBarrel(names []string) []byte {
	var out strings.Builder
	out.WriteString(GeneratedHeader)
	out.WriteByte('\n')
	out.WriteString("export type {")
	if len(names) > 0 {
		out.WriteByte('\n')
		for _, name := range names {
			out.WriteString("  ")
			out.WriteString(name)
			out.WriteString(",\n")
		}
	}
	out.WriteString("} from './types.js';\n")
	return []byte(out.String())
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

func writeType(out *strings.Builder, expr typeExpr, parentPrecedence int, indent string) {
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
		writeObject(out, n, indent)
	case genericType:
		out.WriteString(n.name)
		out.WriteByte('<')
		for i, argument := range n.arguments {
			if i > 0 {
				out.WriteString(", ")
			}
			writeType(out, argument, 0, indent)
		}
		out.WriteByte('>')
	case unionType:
		for i, member := range n.members {
			if i > 0 {
				out.WriteString(" | ")
			}
			writeType(out, member, precedenceUnion, indent)
		}
	case intersectionType:
		for i, member := range n.members {
			if i > 0 {
				out.WriteString(" & ")
			}
			writeType(out, member, precedenceIntersection, indent)
		}
	default:
		panic(fmt.Sprintf("unknown TypeScript type expression %T", expr))
	}
	if parenthesize {
		out.WriteByte(')')
	}
}

func writeObject(out *strings.Builder, object objectType, indent string) {
	out.WriteString("{\n")
	propertyIndent := indent + "  "
	for _, property := range object.properties {
		writeDoc(out, property.description, propertyIndent)
		out.WriteString(propertyIndent)
		out.WriteString(quote(property.name))
		if property.optional {
			out.WriteByte('?')
		}
		out.WriteString(": ")
		writeType(out, property.typeExpr, 0, propertyIndent)
		out.WriteString(";\n")
	}
	out.WriteString(indent)
	out.WriteByte('}')
}

func writeDoc(out *strings.Builder, description, indent string) {
	if description == "" {
		return
	}
	description = sanitizeComment(description)
	lines := strings.Split(description, "\n")
	out.WriteString(indent)
	out.WriteString("/**\n")
	for _, line := range lines {
		out.WriteString(indent)
		out.WriteString(" *")
		if line != "" {
			out.WriteByte(' ')
			out.WriteString(line)
		}
		out.WriteByte('\n')
	}
	out.WriteString(indent)
	out.WriteString(" */\n")
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
