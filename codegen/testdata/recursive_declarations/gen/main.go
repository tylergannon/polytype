// Command gen is the generator program from issue #129. TARGET names the
// package to generate into (noarg, compose, or entrypoint), each of which also
// holds a declaration file; OUT selects one output.
package main

import (
	"fmt"
	"os"

	"github.com/tylergannon/polytype"
	"github.com/tylergannon/polytype/codegen"

	"recursivedeclarations/compose"
	"recursivedeclarations/entrypoint"
	"recursivedeclarations/noarg"
)

func main() {
	target := os.Getenv("TARGET")
	var cfg polytype.Configuration
	switch target {
	case "noarg":
		cfg = config[noarg.Tree, noarg.Node]()
	case "compose":
		cfg = config[compose.Tree, compose.Node]()
	case "entrypoint":
		cfg = config[entrypoint.Tree, entrypoint.Node]()
	default:
		fmt.Fprintln(os.Stderr, "ERROR: TARGET must be noarg, compose, or entrypoint")
		os.Exit(2)
	}
	dir := "./" + target
	opts := []codegen.Option{codegen.Target(dir)}
	switch os.Getenv("OUT") {
	case "schema":
		opts = append(opts, codegen.JSONSchema())
	case "ts":
		opts = append(opts, codegen.TypeScript(dir+"/ts"))
	case "devalue":
		opts = append(opts, codegen.Devalue(dir+"/devalue_gen.go", target, "recursivedeclarations/"+target))
	default:
		opts = append(opts, codegen.GoJSON())
	}
	if err := codegen.Gen(cfg, opts...); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}

// config is the issue's configuration for one package's Tree and Node.
func config[Tree, Node any]() polytype.Configuration {
	return polytype.Compose(
		polytype.Declare[Tree](),
		polytype.SealedUnion[Node]("kind", polytype.Snake),
	)
}
