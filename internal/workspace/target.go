// Package workspace validates za initialization targets, creates them, and
// restores them conservatively when initialization fails.
package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Target is a directory validated as safe to initialize: either an existing
// empty directory or a path that does not exist yet.
type Target struct {
	// Path is the absolute, cleaned target path. Symlinks are not resolved.
	Path string

	missing  []string    // directories to create, outermost first
	created  []string    // directories actually created by Create, outermost first
	identity os.FileInfo // the target directory as seen right after Create
}

// Inspect validates path without modifying the filesystem.
func Inspect(path string) (*Target, error) {
	if path == "" {
		return nil, errors.New("target path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve target path %q: %w", path, err)
	}

	info, err := os.Stat(abs)
	switch {
	case err == nil:
		if !info.IsDir() {
			return nil, fmt.Errorf("target exists and is not a directory: %s", abs)
		}
		if err := requireEmpty(abs); err != nil {
			return nil, err
		}
		return &Target{Path: abs}, nil
	case errors.Is(err, fs.ErrNotExist):
		missing, err := missingDirs(abs)
		if err != nil {
			return nil, err
		}
		return &Target{Path: abs, missing: missing}, nil
	default:
		return nil, fmt.Errorf("inspect target %s: %w", abs, err)
	}
}

// Existed reports whether the target directory existed before Create.
func (t *Target) Existed() bool {
	return len(t.missing) == 0
}

// Create makes the target directory and any missing parents. For a target
// that already existed it re-verifies emptiness right before mutation.
func (t *Target) Create() error {
	for _, dir := range t.missing {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return errors.Join(fmt.Errorf("create target directory: %w", err), t.removeCreatedDirs())
		}
		t.created = append(t.created, dir)
	}
	if t.Existed() {
		if err := requireEmpty(t.Path); err != nil {
			return err
		}
	}

	info, err := os.Stat(t.Path)
	if err != nil {
		return errors.Join(fmt.Errorf("inspect target %s: %w", t.Path, err), t.removeCreatedDirs())
	}
	t.identity = info
	return nil
}

// Cleanup makes a best-effort attempt to restore the target to its state
// before Create.
//
// It removes only the named top-level entries, which the caller created
// inside the target after Create proved it empty, and then the directories
// Create made, each only if it is empty. Anything else is left in place and
// reported. Nothing is removed if the target is no longer the directory
// Create prepared.
func (t *Target) Cleanup(entries []string) error {
	if t.identity == nil {
		return nil
	}
	cur, err := os.Stat(t.Path)
	if err != nil || !os.SameFile(cur, t.identity) {
		return fmt.Errorf("left %s untouched: it is no longer the directory being initialized", t.Path)
	}

	var errs []error
	for _, name := range entries {
		if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
			errs = append(errs, fmt.Errorf("refusing to remove unexpected entry %q", name))
			continue
		}
		if err := os.RemoveAll(filepath.Join(t.Path, name)); err != nil {
			errs = append(errs, err)
		}
	}

	if t.Existed() {
		if err := requireEmpty(t.Path); err != nil {
			errs = append(errs, fmt.Errorf("entries not created by za were left in place: %w", err))
		}
	} else if err := t.removeCreatedDirs(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// removeCreatedDirs removes the directories made by Create, innermost first.
// os.Remove refuses non-empty directories, so unknown content survives.
func (t *Target) removeCreatedDirs() error {
	for i := len(t.created) - 1; i >= 0; i-- {
		if err := os.Remove(t.created[i]); err != nil {
			return fmt.Errorf("left %s in place: %w", t.created[i], err)
		}
		t.created = t.created[:i]
	}
	return nil
}

// missingDirs returns abs and its missing ancestors, outermost first.
func missingDirs(abs string) ([]string, error) {
	var missing []string
	for dir := abs; ; {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("cannot create target %s: %s is not a directory", abs, dir)
			}
			return missing, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("inspect %s: %w", dir, err)
		}
		missing = append([]string{dir}, missing...)

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("cannot create target %s: no existing ancestor directory", abs)
		}
		dir = parent
	}
}

// requireEmpty fails unless dir contains no entries at all, hidden or not.
func requireEmpty(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("inspect target %s: %w", dir, err)
	}
	defer f.Close()

	names, err := f.Readdirnames(1)
	if len(names) > 0 {
		return fmt.Errorf("target directory is not empty: %s", dir)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("inspect target %s: %w", dir, err)
	}
	return nil
}
