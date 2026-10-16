package publication

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletedMutationSurvivesLaterError(t *testing.T) {
	dir := t.TempDir()
	var result Result
	file := filepath.Join(dir, "output")
	require.NoError(t, result.Remove(file))
	require.False(t, result.Changed)
	require.NoError(t, result.Write(file, []byte("first")))
	require.True(t, result.Changed)
	require.Error(t, result.Write(dir, []byte("cannot replace directory")))
	require.True(t, result.Changed)
	result = Result{}
	require.NoError(t, result.Write(file, []byte("first")))
	require.False(t, result.Changed)
	require.NoError(t, result.Remove(file))
	require.True(t, result.Changed)
	_, err := os.Stat(file)
	require.ErrorIs(t, err, os.ErrNotExist)
}
