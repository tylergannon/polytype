package devalue

// isIdentifier is devalue's /^[_$a-zA-Z][_$a-zA-Z0-9]*$/. It is written out
// rather than compiled as a regexp because it runs once per property.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// safeKey renders an object-literal key: bare when it is an identifier, quoted
// otherwise.
func safeKey(key string) string {
	if isIdentifier(key) {
		return key
	}
	return quoteString(key)
}

// safeProp renders a property access on a hoisted name: `.foo` or `["x-y"]`.
func safeProp(key string) string {
	if isIdentifier(key) {
		return "." + key
	}
	return "[" + quoteString(key) + "]"
}

// nameChars is devalue's alphabet for hoisted parameter names.
const nameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_$"

// reservedWords is devalue's `reserved` regexp, which covers the ECMAScript
// reserved words plus the ones reserved by older editions and by Java-derived
// grammars. A generated name that lands on one gets a "0" suffix.
var reservedWords = map[string]bool{
	"do": true, "if": true, "in": true, "for": true, "int": true, "let": true,
	"new": true, "try": true, "var": true, "byte": true, "case": true,
	"char": true, "else": true, "enum": true, "goto": true, "long": true,
	"this": true, "void": true, "with": true, "await": true, "break": true,
	"catch": true, "class": true, "const": true, "final": true, "float": true,
	"short": true, "super": true, "throw": true, "while": true, "yield": true,
	"delete": true, "double": true, "export": true, "import": true,
	"native": true, "return": true, "switch": true, "throws": true,
	"typeof": true, "boolean": true, "default": true, "extends": true,
	"finally": true, "package": true, "private": true, "abstract": true,
	"continue": true, "debugger": true, "function": true, "volatile": true,
	"interface": true, "protected": true, "transient": true,
	"implements": true, "instanceof": true, "synchronized": true,
}

// getName is devalue's `get_name`: a bijective base-54 numeral over nameChars,
// suffixed with "0" when it collides with a reserved word.
func getName(num int) string {
	var name string
	for {
		name = string(nameChars[num%len(nameChars)]) + name
		num = num/len(nameChars) - 1
		if num < 0 {
			break
		}
	}
	if reservedWords[name] {
		return name + "0"
	}
	return name
}
