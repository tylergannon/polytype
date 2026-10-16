// Package publication tracks completed output mutations within one generation.
package publication

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

// Result records whether any output changed, including before a later failure.
type Result struct{ Changed bool }

// Write publishes different bytes atomically; equal bytes preserve the file.
func (r *Result) Write(path string, data []byte) (err error) {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".polytype-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(0644); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	if r != nil {
		r.Changed = true
	}
	return nil
}

// Remove records successful removals, ignoring an already missing output.
func (r *Result) Remove(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil && r != nil {
		r.Changed = true
	}
	return err
}
