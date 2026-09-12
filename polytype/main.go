package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/tylergannon/polytype/internal/builder"
)

func main() {
	if len(os.Args) == 1 {
		handleGen(1)
		return
	}
	if os.Args[1] == "-h" || os.Args[1] == "--help" {
		printGlobalHelp()
		return
	}

	switch os.Args[1] {
	case "gen":
		handleGen(2)
	default:
		handleGen(1)
	}
}

// Prints global help for the script
func printGlobalHelp() {
	fmt.Println("Usage:")
	fmt.Println("  [gen] [options]")
	fmt.Println("\nRun 'gen --help' for options.")
}

type genOptions struct {
	pretty           bool
	target           string
	noChanges        bool
	force            bool
	validate         bool
	formats          string
	typeScriptDir    string
	typeScriptBarrel bool
}

func newGenFlagSet(errorHandling flag.ErrorHandling) (*flag.FlagSet, *genOptions) {
	options := &genOptions{}
	genCmd := flag.NewFlagSet("gen", errorHandling)
	genCmd.BoolVar(&options.pretty, "pretty", false, "Enable pretty output")
	genCmd.StringVar(&options.target, "target", "", "Path to target package (default to local wd)")
	genCmd.BoolVar(&options.noChanges, "no-changes", false, "Fail if any schema or requested TypeScript output changes are detected")
	genCmd.BoolVar(&options.force, "force", false, "Force regeneration of schemas and requested TypeScript output, and allow removal of generated validation methods")
	genCmd.BoolVar(&options.validate, "validate", false, "Generate schema validation methods for the selected formats")
	genCmd.StringVar(&options.formats, "formats", "json", "Generated decoding and validation formats: json or both")
	genCmd.StringVar(&options.typeScriptDir, "typescript", "", "Generate structural TypeScript declarations in this directory")
	genCmd.BoolVar(&options.typeScriptBarrel, "typescript-barrel", false, "Generate an index.ts type-only export (requires --typescript)")
	return genCmd, options
}

func handleGen(firstArg int) {
	genCmd, options := newGenFlagSet(flag.ExitOnError)

	// Check if --help was requested
	if len(os.Args) > 2 && os.Args[2] == "--help" {
		fmt.Println("Usage: gen [options]")
		fmt.Println("\nOptions:")
		genCmd.PrintDefaults()
		return
	}
	_ = genCmd.Parse(os.Args[firstArg:])
	if options.target == "" {
		var err error
		if options.target, err = os.Getwd(); err != nil {
			log.Fatal(err)
		}
	} else if st, err := os.Stat(options.target); err != nil {
		log.Fatal(err)
	} else if !st.IsDir() {
		log.Fatalf("%s is not a directory", options.target)
	}

	unmarshalFormats, err := parseUnmarshalFormats(options.formats)
	if err != nil {
		log.Fatal(err)
	}

	// Check environment variable
	options.noChanges = options.noChanges || os.Getenv("JSONSCHEMA_NO_CHANGES") != ""

	if options.force && options.noChanges {
		log.Fatal("Cannot use --force and --no-changes together")
	}

	if err = builder.Run(builder.BuilderArgs{
		TargetDir:        options.target,
		Pretty:           options.pretty,
		NoChanges:        options.noChanges,
		Force:            options.force,
		Validate:         options.validate,
		TypeScriptDir:    options.typeScriptDir,
		TypeScriptBarrel: options.typeScriptBarrel,
		UnmarshalFormats: unmarshalFormats,
	}); err != nil {
		log.Fatal(err)
	}
}

func parseUnmarshalFormats(value string) (builder.UnmarshalFormats, error) {
	formats := builder.UnmarshalFormats(value)
	switch formats {
	case builder.UnmarshalFormatsJSON, builder.UnmarshalFormatsBoth:
		return formats, nil
	default:
		return "", fmt.Errorf("invalid --formats value %q: expected json or both", value)
	}
}
