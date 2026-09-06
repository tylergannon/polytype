// Command generate writes backend-only TypeScript conformance fixtures.
package main

import (
	"fmt"
	"go/constant"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tylergannon/polytype/typegrammar"
	"github.com/tylergannon/polytype/internal/typescript"
)

const packagePath = "example.com/projection-probe"

var permissiveType = regexp.MustCompile(`\b(?:any|unknown)\b`)

func main() {
	if len(os.Args) != 2 {
		fail("usage: go run ./tests/typescript/projection/generate <output-directory>")
	}
	if err := generate(filepath.Clean(os.Args[1])); err != nil {
		fail("TypeScript projection conformance: %v", err)
	}
}

func generate(outputDir string) error {
	empty, err := typescript.Generate(nil, typescript.Options{Barrel: true})
	if err != nil {
		return fmt.Errorf("generate empty graph: %w", err)
	}
	if len(empty) != 2 || string(empty[0].Content) != typescript.GeneratedHeader+"export {};\n" {
		return fmt.Errorf("empty graph did not emit an importable types module and barrel")
	}
	if err := writeFiles(filepath.Join(outputDir, "empty"), empty); err != nil {
		return err
	}

	discriminator := "kind\"\\\n雪"
	defs := typegrammar.Definitions{
		definition("Empty", &typegrammar.Object{}),
		definition("Exact", &typegrammar.Enum{
			GoType: name("Exact"),
			Kind:   typegrammar.Int64,
			Mode:   typegrammar.EnumValues,
			Members: []typegrammar.EnumMember{
				{Name: "Large", Value: integer("9007199254740992")},
			},
		}),
		{
			Name:        name("Payload"),
			Description: "Payload comment closes */ then continues.\u2028Next line separator.",
			Type: &typegrammar.Object{Fields: []typegrammar.Field{
				{GoName: "Kind", JSONName: discriminator, Value: &typegrammar.Optional{Type: &typegrammar.Scalar{Kind: typegrammar.String}}},
				{GoName: "Value", JSONName: "value", Description: "Control \u0001 is escaped.", Value: &typegrammar.Required{Type: &typegrammar.Scalar{Kind: typegrammar.String}}},
			}},
		},
		definition("Owner", &typegrammar.Object{Fields: []typegrammar.Field{{
			GoName:   "Event",
			JSONName: "event",
			Value: &typegrammar.Union{
				Interface:     name("Event"),
				Discriminator: discriminator,
				Variants: []typegrammar.Variant{{
					Implementation: name("Payload"),
					Tag:            "",
				}},
			},
		}}}),
		definition("雪", &typegrammar.Object{}),
		definition("_u96EA_", &typegrammar.Object{}),
	}
	edge, err := typescript.Generate(defs, typescript.Options{Barrel: true})
	if err != nil {
		return fmt.Errorf("generate edge graph: %w", err)
	}
	if len(edge) != 2 {
		return fmt.Errorf("edge graph emitted %d files, want types plus barrel", len(edge))
	}
	types := string(edge[0].Content)
	for description, required := range map[string]string{
		"exact large integer":                "export type Exact = 9007199254740992;",
		"escaped comment terminator":         `Payload comment closes *\/ then continues.\u2028Next line separator.`,
		"escaped control in field comment":   `Control \u0001 is escaped.`,
		"escaped discriminator property":     `"kind\"\\\n雪": "";`,
		"field-local discriminator omission": `Omit<Payload, "kind\"\\\n雪">`,
	} {
		if !strings.Contains(types, required) {
			return fmt.Errorf("generated types lack %s (%q)", description, required)
		}
	}
	if count := strings.Count(types, "export type _u96EA_$"); count != 2 {
		return fmt.Errorf("encoded Unicode name collision produced %d declarations, want 2", count)
	}
	if permissiveType.MatchString(types) {
		return fmt.Errorf("generated types contain an any/unknown fallback")
	}
	if err := writeFiles(filepath.Join(outputDir, "edge"), edge); err != nil {
		return err
	}

	inexact := typegrammar.Definitions{definition("Inexact", &typegrammar.Enum{
		GoType: name("Inexact"),
		Kind:   typegrammar.Int64,
		Mode:   typegrammar.EnumValues,
		Members: []typegrammar.EnumMember{{
			Name:  "Inexact",
			Value: integer("9007199254740993"),
		}},
	})}
	files, err := typescript.Generate(inexact, typescript.Options{})
	if err == nil || !strings.Contains(err.Error(), "not exactly representable") || files != nil {
		return fmt.Errorf("inexact integer did not fail atomically with its required diagnostic: files=%v err=%v", files, err)
	}

	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func name(value string) typegrammar.Name {
	return typegrammar.Name{PackagePath: packagePath, Name: value}
}

func definition(value string, typ typegrammar.Type) typegrammar.Definition {
	return typegrammar.Definition{Name: name(value), Type: typ}
}

func integer(value string) constant.Value {
	return constant.MakeFromLiteral(value, token.INT, 0)
}

func writeFiles(dir string, files []typescript.File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, file := range files {
		path := filepath.Join(dir, file.Name)
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}
