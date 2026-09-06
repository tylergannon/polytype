package codegen

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// exportedIdentifier turns a Go type's local name into an exported identifier
// suitable as the suffix of a generated function name.
func exportedIdentifier(name string) string {
	identifier := safeIdentifier(name)
	if identifier == "" {
		return "X"
	}
	runes := []rune(identifier)
	if !unicode.IsUpper(runes[0]) {
		return "X" + identifier
	}
	return identifier
}

// safeIdentifier rewrites every rune a Go identifier cannot carry into an
// escape, so distinct names never collapse onto one another.
func safeIdentifier(name string) string {
	var out strings.Builder
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || i > 0 && unicode.IsDigit(r) {
			if r < utf8.RuneSelf {
				out.WriteRune(r)
				continue
			}
		}
		fmt.Fprintf(&out, "_u%X_", r)
	}
	return out.String()
}

// escapePointer escapes a JSON Pointer reference token (RFC 6901).
func escapePointer(segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	return strings.ReplaceAll(segment, "/", "~1")
}
