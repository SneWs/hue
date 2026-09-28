// Package config stores the paired Hue Bridge application key.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Bridge is the paired bridge this user can control.
type Bridge struct {
	ID        string `json:"bridgeId"`
	IP        string `json:"ip"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	ClientKey string `json:"clientKey,omitempty"`
}

// Path is ~/.config/hue/bridge.json. The application key is a credential,
// so the file is created with mode 0600.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hue", "bridge.json"), nil
}

// Load returns nil when the user has not paired a bridge yet.
func Load() (*Bridge, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var bridge Bridge
	if err := json.Unmarshal(data, &bridge); err != nil {
		return nil, err
	}
	if bridge.Username == "" || bridge.IP == "" {
		return nil, nil
	}
	return &bridge, nil
}

// Save writes the bridge credential atomically.
func Save(bridge Bridge) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bridge, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writePrivateFile(path, data)
}

// writePrivateFile replaces path by renaming an exclusive temp file in the
// same directory. A predictable name such as path+".tmp" is not used, because
// WriteFile would follow a symlink planted at that name.
func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".bridge-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Forget removes the stored credential.
func Forget() error {
	path, err := Path()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
