package main

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tylergannon/polytype/internal/testutils"
)

func TestGenTypeScriptFlags(t *testing.T) {
	t.Parallel()

	flags, options := newGenFlagSet(flag.ContinueOnError)
	err := flags.Parse([]string{
		"--typescript", "web/generated",
		"--typescript-barrel",
	})
	require.NoError(t, err)
	require.Equal(t, "web/generated", options.typeScriptDir)
	require.True(t, options.typeScriptBarrel)
}

func TestGenTypeScriptFlagsDefaultToDisabled(t *testing.T) {
	t.Parallel()

	flags, options := newGenFlagSet(flag.ContinueOnError)
	require.NoError(t, flags.Parse(nil))
	require.Empty(t, options.typeScriptDir)
	require.False(t, options.typeScriptBarrel)
}

// TestGenCommandRejectsInvalidFluentFieldAssociationWithSourcePosition runs
// the actual polytype binary (not an in-process builder.Run call)
// against the checked-in fluent_field_mismatch scanner fixture
// (internal/syntax/testfixtures/fluent_field_mismatch), whose .StringerEnum(...)
// chain link names a field on a type other than its Declare(...) root. This
// proves the command itself - not just the internal scanner/builder APIs -
// fails with a non-zero exit and a source-positioned diagnostic naming the
// offending file for an invalid fluent chain.
func TestGenCommandRejectsInvalidFluentFieldAssociationWithSourcePosition(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	exitCode, _, stderr, err := testutils.RunCommand("go", cwd, "run", ".", "-target", "../internal/syntax/testfixtures/fluent_field_mismatch")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "stderr:\n%s", stderr)
	require.Contains(t, stderr, "polytype.Declare: .StringerEnum expects a field selector on Owner{}")
	require.Contains(t, stderr, "fluent_field_mismatch/fixture.go")
}

// TestGenCommandRejectsEnumMarkerWithPointerReceiver runs the actual
// polytype binary against the checked-in enum_marker_pointer_receiver
// fixture, proving the command itself fails with a non-zero exit and a
// source-positioned diagnostic naming the mis-marked enum type.
func TestGenCommandRejectsEnumMarkerWithPointerReceiver(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	exitCode, _, stderr, err := testutils.RunCommand("go", cwd, "run", ".", "-target", "../internal/syntax/testfixtures/enum_marker_pointer_receiver")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "stderr:\n%s", stderr)
	require.Contains(t, stderr, "enum marker on Status at ")
	require.Contains(t, stderr, "must use a value receiver: func (Status) enum()")
	require.Contains(t, stderr, "enum_marker_pointer_receiver/fixture.go")
}

// TestGenCommandRejectsUnsealedInterfaceField runs the actual polytype
// binary against the checked-in unsealed_interface fixture, proving the
// command itself fails with a non-zero exit and a source-positioned
// diagnostic naming both the field and the non-sealed interface.
func TestGenCommandRejectsUnsealedInterfaceField(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	exitCode, _, stderr, err := testutils.RunCommand("go", cwd, "run", ".", "-target", "../internal/syntax/testfixtures/unsealed_interface")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "stderr:\n%s", stderr)
	require.Contains(t, stderr, "field Drawing.Shape at ")
	require.Contains(t, stderr, "interface Shape at ")
	require.Contains(t, stderr, "is not sealed")
	require.Contains(t, stderr, "unsealed_interface/fixture.go")
}

// TestGenCommandRejectsSealedUnionDeclaredOutsideInterfacePackage runs the
// actual polytype binary against the checked-in sealed_union_foreign_package
// fixture, proving the command itself fails with a non-zero exit and a
// source-positioned diagnostic naming the interface and the package that
// must hold the declaration.
func TestGenCommandRejectsSealedUnionDeclaredOutsideInterfacePackage(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	exitCode, _, stderr, err := testutils.RunCommand("go", cwd, "run", ".", "-target", "../internal/syntax/testfixtures/sealed_union_foreign_package")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "stderr:\n%s", stderr)
	require.Contains(t, stderr, "polytype.SealedUnion[Animal] at ")
	require.Contains(t, stderr, "must be declared in package github.com/tylergannon/polytype/internal/syntax/testfixtures/sealed_union_foreign_package/animals")
	require.Contains(t, stderr, "sealed_union_foreign_package/fixture.go")
}
