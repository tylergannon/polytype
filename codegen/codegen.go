// Package codegen runs polytype generation from executable configuration
// values. It does not require a schema.go registration file in the target
// package.
package codegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tylergannon/polytype"
	devaluegen "github.com/tylergannon/polytype/devalue/codegen"
	"github.com/tylergannon/polytype/internal/builder"
	"github.com/tylergannon/polytype/typescript"
)

// Options selects outputs for Generate. JSON Schema is optional: TypeScript,
// devalue, and generated Go JSON codecs can be requested independently.
type Options struct {
	TargetDir string
	Pretty    bool
	Force     bool

	JSONSchema bool
	GoCode     bool
	Validate   bool

	TypeScript *TypeScriptOptions
	Devalue    *DevalueOptions
}

// TypeScriptOptions selects structural TypeScript output.
type TypeScriptOptions struct {
	Dir    string
	Barrel bool
}

// DevalueOptions selects generated devalue Go codecs.
type DevalueOptions struct {
	File        string
	PackageName string
	ImportPath  string
}

// Option modifies the convenience Gen request.
type Option func(*Options)

// Target loads and writes the configured package at dir.
func Target(dir string) Option { return func(o *Options) { o.TargetDir = dir } }

// Pretty formats JSON Schema output with indentation.
func Pretty() Option { return func(o *Options) { o.Pretty = true } }

// JSONSchema selects JSON Schema files. If the declaration carries an
// entrypoint, its Go accessor is emitted as well.
func JSONSchema() Option { return func(o *Options) { o.JSONSchema = true } }

// GoJSON selects generated MarshalJSON and UnmarshalJSON support required by
// configured enums and sealed unions, without selecting JSON Schema.
func GoJSON() Option { return func(o *Options) { o.GoCode = true } }

// Validation selects JSON Schema, its Go accessors, and generated validators.
func Validation() Option {
	return func(o *Options) {
		o.JSONSchema = true
		o.GoCode = true
		o.Validate = true
	}
}

// TypeScript writes structural TypeScript declarations to dir.
func TypeScript(dir string, barrel ...bool) Option {
	return func(o *Options) {
		o.TypeScript = &TypeScriptOptions{Dir: dir, Barrel: len(barrel) > 0 && barrel[0]}
	}
}

// Devalue writes strict devalue codecs to file.
func Devalue(file, packageName, importPath string) Option {
	return func(o *Options) {
		o.Devalue = &DevalueOptions{File: file, PackageName: packageName, ImportPath: importPath}
	}
}

// Gen runs generation with functional options. With no options,
// Declare(T.Schema) preserves the traditional schema-and-accessor behavior.
// A declaration without an entrypoint must select at least one output.
func Gen(config polytype.Configuration, options ...Option) error {
	var opts Options
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return Generate(config, opts)
}

// Generate runs the selected backends from the same declaration value used
// by build-tagged bindings.
func Generate(config polytype.Configuration, opts Options) error {
	spec, err := polytype.ResolveConfiguration(config)
	if err != nil {
		return err
	}
	if opts.TargetDir == "" {
		opts.TargetDir = spec.Declarations[0].Type.PackagePath
	}

	configured, hasEntrypoint, err := builderConfig(spec)
	if err != nil {
		return err
	}
	if !outputsSelected(opts) {
		if !hasEntrypoint {
			return errors.New("codegen: no output selected; use JSONSchema, GoJSON, TypeScript, or Devalue")
		}
		opts.JSONSchema = true
		opts.GoCode = true
	}
	if opts.Validate && !hasEntrypoint {
		return errors.New("codegen: validation requires a declaration entrypoint, such as Declare(Person.Schema)")
	}
	if opts.Validate {
		for _, declaration := range spec.Declarations {
			if declaration.EntrypointName == "" {
				return fmt.Errorf("codegen: validation for %s requires a declaration entrypoint", declaration.Type.Name)
			}
		}
	}
	for _, declaration := range spec.Declarations {
		for _, rule := range declaration.Rules {
			switch rule.Kind {
			case polytype.RuleAccessor, polytype.RuleMethod, polytype.RuleFunction, polytype.RuleRenderProviders:
				if !opts.JSONSchema {
					return fmt.Errorf("codegen: %s rule on %s requires JSON Schema output", rule.Kind, declaration.Type.Name)
				}
				if declaration.EntrypointName == "" {
					return fmt.Errorf("codegen: %s rule on %s requires a declaration entrypoint", rule.Kind, declaration.Type.Name)
				}
			}
		}
	}

	b, err := builder.LoadProgrammatic(opts.TargetDir, configured, opts.JSONSchema || opts.GoCode)
	if err != nil {
		return err
	}
	b.Pretty = opts.Pretty
	b.Validate = opts.Validate
	b.GenerateSchemas = opts.JSONSchema && hasEntrypoint
	if err := b.ApplyTransforms(); err != nil {
		return err
	}

	needGrammar := opts.TypeScript != nil || opts.Devalue != nil
	if needGrammar {
		defs, roots, err := b.ConfiguredTypeDefinitions()
		if err != nil {
			return err
		}
		if opts.TypeScript != nil {
			result, err := typescript.Generate(defs, typescript.Options{Barrel: opts.TypeScript.Barrel})
			if err != nil {
				return err
			}
			for _, file := range result.Files {
				if err := writeOutput(filepath.Join(opts.TypeScript.Dir, file.Name), file.Content); err != nil {
					return err
				}
			}
		}
		if opts.Devalue != nil {
			source, err := devaluegen.Generate(defs, roots, devaluegen.Options{
				PackageName: opts.Devalue.PackageName,
				ImportPath:  opts.Devalue.ImportPath,
			})
			if err != nil {
				return err
			}
			if err := writeOutput(opts.Devalue.File, source); err != nil {
				return err
			}
		}
	}

	if opts.JSONSchema {
		if _, _, err := b.RenderSchemas(false, opts.Force); err != nil {
			return err
		}
	}
	if (opts.GoCode && b.HasGeneratedJSONCode()) || (opts.JSONSchema && hasEntrypoint) {
		if err := b.RenderGoCode(); err != nil {
			return err
		}
	}
	return nil
}

func outputsSelected(opts Options) bool {
	return opts.JSONSchema || opts.GoCode || opts.TypeScript != nil || opts.Devalue != nil
}

func builderConfig(spec polytype.ConfigurationSpec) (builder.ProgrammaticConfig, bool, error) {
	var out builder.ProgrammaticConfig
	hasEntrypoint := false
	packagePath := ""
	for _, declaration := range spec.Declarations {
		if packagePath == "" {
			packagePath = declaration.Type.PackagePath
		}
		if declaration.Type.PackagePath != packagePath {
			return builder.ProgrammaticConfig{}, false, fmt.Errorf("codegen: one generation request cannot span packages %s and %s", packagePath, declaration.Type.PackagePath)
		}
		configured := builder.ConfiguredDeclaration{
			PackagePath:    declaration.Type.PackagePath,
			TypeName:       declaration.Type.Name,
			Pointer:        declaration.Type.Pointer,
			Entrypoint:     declaration.EntrypointName,
			EntrypointFunc: declaration.EntrypointFunc,
		}
		if configured.Entrypoint != "" {
			hasEntrypoint = true
		}
		for _, rule := range declaration.Rules {
			kind, err := builderRuleKind(rule.Kind)
			if err != nil {
				return builder.ProgrammaticConfig{}, false, err
			}
			configured.Rules = append(configured.Rules, builder.ConfiguredRule{
				Kind:             kind,
				FieldName:        rule.Field.Name,
				ProviderName:     rule.ProviderName,
				ProviderIsMethod: rule.ProviderIsMethod,
			})
		}
		out.Declarations = append(out.Declarations, configured)
	}
	for _, union := range spec.SealedUnions {
		out.SealedUnions = append(out.SealedUnions, builder.ConfiguredUnion{
			PackagePath: union.Type.PackagePath, TypeName: union.Type.Name, Discriminator: union.Discriminator, Inflect: union.Inflect,
		})
	}
	return out, hasEntrypoint, nil
}

func builderRuleKind(kind polytype.RuleKind) (string, error) {
	switch kind {
	case polytype.RuleAccessor:
		return "WithStructAccessorMethod", nil
	case polytype.RuleMethod:
		return "WithStructFunctionMethod", nil
	case polytype.RuleFunction:
		return "WithFunction", nil
	case polytype.RuleStringerEnum:
		return "WithStringerEnum", nil
	case polytype.RuleRef:
		return "AsRef", nil
	case polytype.RuleRenderProviders:
		return "WithRenderProviders", nil
	default:
		return "", fmt.Errorf("codegen: unknown declaration rule %q", kind)
	}
}

func writeOutput(path string, content []byte) error {
	if path == "" {
		return errors.New("codegen: output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
