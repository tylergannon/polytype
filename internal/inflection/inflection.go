package inflection

import (
	"strings"
	"unicode"
)

// Pascal preserves a Go type name exactly. Go named types already use
// PascalCase, and preserving initialisms keeps this function wire-compatible
// with polytype's historical discriminator values.
func Pascal(name string) string { return name }

// Snake converts a Go type name to lower snake_case.
func Snake(name string) string {
	words := split(name)
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, "_")
}

// Camel converts a Go type name to lower camelCase.
func Camel(name string) string {
	words := split(name)
	if len(words) == 0 {
		return ""
	}
	for i := range words {
		words[i] = strings.ToLower(words[i])
		if i > 0 {
			words[i] = upperFirst(words[i])
		}
	}
	return strings.Join(words, "")
}

func split(name string) []string {
	runes := []rune(name)
	var words []string
	start := -1
	flush := func(end int) {
		if start >= 0 && start < end {
			words = append(words, string(runes[start:end]))
		}
		start = -1
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		prev := runes[i-1]
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower)) {
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	return words
}

func upperFirst(value string) string {
	runes := []rune(value)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}
