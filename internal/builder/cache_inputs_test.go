package builder_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

// TestFixtureDependenciesIncludeTheRuntime pins what TestBasic tracks: its
// fixtures run against the root package's runtime types (Optional,
// Nullable), which this test binary does not otherwise depend on.
func TestFixtureDependenciesIncludeTheRuntime(t *testing.T) {
	t.Parallel()
	imports, err := testutils.TrackedFixtureDependencies("testfixtures", "github.com/tylergannon/polytype", fixtureModulePath)
	require.NoError(t, err)
	require.Contains(t, imports, "github.com/tylergannon/polytype")
}
