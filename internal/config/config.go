// Package config persists the last-used server so it doesn't need to be
// typed on every invocation.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// legacyName is the pre-0.2 config file kept in the working directory.
// It is still read as a fallback so existing installs migrate silently.
const legacyName = ".shortwave.json"

// Config is the persisted user state.
type Config struct {
	LastServer string  `json:"last_server,omitempty"`
	LastFreq   float64 `json:"last_freq,omitempty"`
	LastMode   string  `json:"last_mode,omitempty"`
}

// Path returns the config file location, preferring the OS user-config dir
// and falling back to the working directory when it is unavailable.
func Path() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "shortwave", "config.json")
	}
	return filepath.Join(".", legacyName)
}

// legacyPath is the old working-directory config file.
func legacyPath() string {
	return filepath.Join(".", legacyName)
}

// Load reads stored state. It checks the new location first, then the legacy
// working-directory file, and returns an empty Config when neither exists.
func Load() *Config {
	c := &Config{}
	if b, err := os.ReadFile(Path()); err == nil {
		_ = json.Unmarshal(b, c) // corrupt config falls back to defaults
		if c.LastServer != "" || c.LastFreq != 0 || c.LastMode != "" {
			return c
		}
	}
	if b, err := os.ReadFile(legacyPath()); err == nil {
		_ = json.Unmarshal(b, c) // corrupt config falls back to defaults
	}
	return c
}

// Save writes state with owner-only permissions, creating parent dirs.
func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
