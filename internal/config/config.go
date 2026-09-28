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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
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
