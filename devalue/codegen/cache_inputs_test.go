package codegen_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/devalue/v5"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestFixtureDependenciesIncludeTheRuntime pins what
// TestGeneratedCodecsCompileAndRun tracks: its fixture runs against the
// standalone devalue runtime that generated codecs use. Importing it here
// also keeps the fixture dependency in the root module after go mod tidy.
func TestFixtureDependenciesIncludeTheRuntime(t *testing.T) {
	t.Parallel()
	imports, err := testutils.TrackedFixtureDependencies("testdata", reflect.TypeFor[devalue.Object]().PkgPath())
	require.NoError(t, err)
	require.Contains(t, imports, reflect.TypeFor[devalue.Object]().PkgPath())
}
