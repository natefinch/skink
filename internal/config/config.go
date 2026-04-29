// Package config loads and saves the skink user config file.
//
// The config is a small YAML document stored at ~/.skink/config.yaml
// (see package paths). It tracks the URL of the user's skills repo and
// the checkout directory on disk.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the persisted skink configuration.
type Config struct {
	SkillsRepo  string `yaml:"skills_repo"`
	CheckoutDir string `yaml:"checkout_dir"`
}

// ErrNotFound is returned by Load when no config file exists yet. Callers
// use it to trigger the first-run flow.
var ErrNotFound = errors.New("config: not found")

// Load reads the config at path. If the file does not exist, it returns
// ErrNotFound so callers can distinguish "first run" from "broken file".
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrNotFound
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return c, nil
}

// Save writes cfg to path atomically (write to tmp, rename). Parent dirs are
// created with 0o755.
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("config: tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// Validate ensures required fields are set.
func (c Config) Validate() error {
	if c.SkillsRepo == "" {
		return errors.New("config: skills_repo is required")
	}
	if c.CheckoutDir == "" {
		return errors.New("config: checkout_dir is required")
	}
	return nil
}
