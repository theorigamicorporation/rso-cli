// Package config keeps what the CLI remembers between runs: the chosen
// tenant and endpoints in config.yaml, and the sign-in in credentials.json.
// Both live in the user's configuration directory; the credentials file is
// readable by its owner only.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/theorigamicorporation/rsodev-go/platform"
)

// Settings is the content of config.yaml.
type Settings struct {
	Tenant     string `yaml:"tenant,omitempty"`
	TenantName string `yaml:"tenant_name,omitempty"`
	Region     string `yaml:"region,omitempty"`
	APIURL     string `yaml:"api_url,omitempty"`
	TokenURL   string `yaml:"token_url,omitempty"`
	ClientID   string `yaml:"client_id,omitempty"`
}

// Store reads and writes the files in one directory.
type Store struct {
	Dir string
}

// Dir is where the CLI keeps its files: RSO_CONFIG_DIR, or "rso" under the
// user's configuration directory.
func Dir(env func(string) string) (string, error) {
	if dir := env("RSO_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no configuration directory: %w", err)
	}
	return filepath.Join(base, "rso"), nil
}

func (s Store) settingsPath() string    { return filepath.Join(s.Dir, "config.yaml") }
func (s Store) credentialsPath() string { return filepath.Join(s.Dir, "credentials.json") }

// Settings returns the saved settings; none saved yet is not an error.
func (s Store) Settings() (Settings, error) {
	var settings Settings
	raw, err := os.ReadFile(s.settingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err := yaml.Unmarshal(raw, &settings); err != nil {
		return settings, fmt.Errorf("%s: %w", s.settingsPath(), err)
	}
	return settings, nil
}

// SaveSettings writes config.yaml.
func (s Store) SaveSettings(settings Settings) error {
	raw, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}
	return s.write(s.settingsPath(), raw)
}

// Credentials returns the saved sign-in; none saved is not an error.
func (s Store) Credentials() (platform.TokenSet, error) {
	var set platform.TokenSet
	raw, err := os.ReadFile(s.credentialsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return set, nil
	}
	if err != nil {
		return set, err
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return platform.TokenSet{}, fmt.Errorf("%s: %w", s.credentialsPath(), err)
	}
	return set, nil
}

// SaveCredentials writes credentials.json, readable by its owner only.
func (s Store) SaveCredentials(set platform.TokenSet) error {
	raw, err := json.Marshal(set) //nolint:gosec // keeping the sign-in is the point; the file is private to its owner
	if err != nil {
		return err
	}
	return s.write(s.credentialsPath(), raw)
}

// DeleteCredentials forgets the sign-in. Nothing to forget is not an error.
func (s Store) DeleteCredentials() error {
	err := os.Remove(s.credentialsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// write replaces a file through a temporary one, so a crash never leaves a
// half-written file, and keeps it private to its owner.
func (s Store) write(path string, content []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
