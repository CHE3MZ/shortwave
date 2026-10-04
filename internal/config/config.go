// Package config persists the last-used server so it doesn't need to be
// typed on every invocation.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the persisted user state.
type Config struct {
	LastServer string  `json:"last_server,omitempty"`
	LastFreq   float64 `json:"last_freq,omitempty"`
	LastMode   string  `json:"last_mode,omitempty"`
}

// Path returns the config file location in the OS user-config dir,
// falling back to the working directory only when it is unavailable
// (practically never on supported platforms).
func Path() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "shortwave", "config.json")
	}
	return filepath.Join(".", "shortwave-config.json")
}

// Load reads stored state, returning an empty Config when none exists.
// Nothing requires the file to exist: callers treat empty as "ask once".
func Load() *Config {
	c := &Config{}
	if b, err := os.ReadFile(Path()); err == nil {
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
