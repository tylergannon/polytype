package testutils

import (
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		var (
			relPath string
			dstPath string
			srcFile *os.File
			dstFile *os.File
		)

		// Create relative path
		if relPath, err = filepath.Rel(src, path); err != nil {
			return err
		}
		dstPath = filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		if srcFile, err = os.Open(path); err != nil {
			return err
		}
		defer objClose(srcFile)

		if dstFile, err = os.Create(dstPath); err != nil {
			return err
		}
		defer objClose(dstFile)

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func objClose(obj interface{ Close() error }) {
	if err := obj.Close(); err != nil {
		log.Printf("error closing %T: %v\n", obj, err)
	}
}

func RunCommand(command string, workDir string, args ...string) (exitCode int, stdout, stderr string, err error) {
	return RunCommandEnv(command, workDir, nil, args...)
}

// RunCommandEnv is RunCommand with extra "KEY=VALUE" entries appended to the
// process environment. Passing a subprocess its environment directly keeps the
// caller free of t.Setenv, which Go forbids alongside t.Parallel.
func RunCommandEnv(command string, workDir string, env []string, args ...string) (exitCode int, stdout, stderr string, err error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, "", "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, "", "", err
	}

	if err := cmd.Start(); err != nil {
		return 0, "", "", err
	}

	stdoutBytes, err := io.ReadAll(stdoutPipe)
	if err != nil {
		return 0, "", "", err
	}
	stderrBytes, err := io.ReadAll(stderrPipe)
	if err != nil {
		return 0, "", "", err
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return exitErr.ExitCode(), string(stdoutBytes), string(stderrBytes), nil
		}
		return 0, string(stdoutBytes), string(stderrBytes), err
	}

	return cmd.ProcessState.ExitCode(), string(stdoutBytes), string(stderrBytes), nil
}
