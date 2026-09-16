package codegen_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestFixtureDependenciesIncludeTheRuntime pins what
// TestGeneratedCodecsCompileAndRun tracks: its fixture runs against the
// devalue runtime, which this test binary does not otherwise depend on.
func TestFixtureDependenciesIncludeTheRuntime(t *testing.T) {
	t.Parallel()
	imports, err := testutils.TrackedFixtureDependencies("testdata", "github.com/tylergannon/polytype")
	require.NoError(t, err)
	require.Contains(t, imports, "github.com/tylergannon/polytype/devalue")
}
