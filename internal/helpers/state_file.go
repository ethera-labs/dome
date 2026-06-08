package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// State files live alongside the test source so make clean and git-ignore
// rules in the existing repo can target them as a group.
func stateDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "test")
}

// LoadJSONState reads state from <stateDir>/<name>. Returns (nil, nil) if the
// file doesn't exist — the typical "first run" signal.
func LoadJSONState(name string, out any) (found bool, err error) {
	data, err := os.ReadFile(filepath.Join(stateDir(), name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read state %s: %w", name, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("decode state %s: %w", name, err)
	}
	return true, nil
}

// SaveJSONState writes state to <stateDir>/<name>.
func SaveJSONState(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state %s: %w", name, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(stateDir(), name), data, 0o644); err != nil {
		return fmt.Errorf("write state %s: %w", name, err)
	}
	return nil
}

// DeleteJSONState removes the named state file. Missing-file is not an error.
func DeleteJSONState(name string) error {
	err := os.Remove(filepath.Join(stateDir(), name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
