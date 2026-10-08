package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	_ "github.com/tylergannon/devalue/v5"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestFixtureDependenciesIncludeTheRuntime pins what
// TestGeneratedCodecsCompileAndRun tracks: its fixture runs against the
// standalone devalue runtime that generated codecs use. Importing it here
// also keeps the fixture dependency in the root module after go mod tidy.
func TestFixtureDependenciesIncludeTheRuntime(t *testing.T) {
	t.Parallel()
	imports, err := testutils.TrackedFixtureDependencies("testdata", "github.com/tylergannon/devalue/v5")
	require.NoError(t, err)
	require.Contains(t, imports, "github.com/tylergannon/devalue/v5")
}
