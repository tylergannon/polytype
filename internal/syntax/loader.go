package syntax

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"
)

const BuildTag = "jsonschema"

const PackageLoadNeeds = packages.NeedDeps |
	packages.NeedModule |
	packages.NeedName |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedCompiledGoFiles |
	packages.NeedFiles

var DefaultPackageCfg = &packages.Config{
	Mode:       PackageLoadNeeds,
	Tests:      false,
	BuildFlags: []string{"-tags=" + BuildTag},
}

// LoadFrom resolves pattern (an import path) with the go command running in
// dir, so a package outside the current module -- a dependency of a caller's
// own loaded package -- resolves against that package's module rather than the
// process working directory.
func LoadFrom(dir, pattern string) ([]*decorator.Package, error) {
	config := *DefaultPackageCfg
	config.Dir = dir
	return decorator.Load(&config, pattern)
}

func Load(path string) ([]*decorator.Package, error) {
	targetDir, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve package directory %s: %w", path, err)
	}
	config := *DefaultPackageCfg
	info, statErr := os.Stat(targetDir)
	switch {
	case statErr == nil && info.IsDir():
		config.Dir = targetDir
		return decorator.Load(&config, ".")
	case statErr == nil:
		return nil, fmt.Errorf("package path %s is not a directory", path)
	case errors.Is(statErr, os.ErrNotExist):
		// Remote dependencies are recursively loaded by import path rather than
		// filesystem directory. Keep that established caller contract.
		return decorator.Load(&config, path)
	default:
		return nil, fmt.Errorf("inspect package path %s: %w", path, statErr)
	}
}
