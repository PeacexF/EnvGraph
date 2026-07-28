// Package config loads the optional .envgraph.yml
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Names are the file names looked for in the scan root, in order.
var Names = []string{".envgraph.yml", ".envgraph.yaml"}

// SystemVariables are set by the shell, the OS, or the CI runner rather than by the project, so reporting them as unused is noise
// Ignored by default. Set `systemVariables: false` to see them.
var SystemVariables = []string{
	"CI",
	"EDITOR",
	"GOPATH",
	"GOROOT",
	"HOME",
	"HOSTNAME",
	"LANG",
	"LC_ALL",
	"LOGNAME",
	"NO_COLOR",
	"OLDPWD",
	"PATH",
	"PWD",
	"SHELL",
	"SHLVL",
	"TERM",
	"TMPDIR",
	"TZ",
	"USER",

	// Set by the GitHub Actions runner rather than by the workflow.
	"ACTIONS_*",
	"GITHUB_*",
	"RUNNER_*",
}

// Config is the parsed .envgraph.yml.
type Config struct {
	// Exclude lists directory names to skip, matching --exclude.
	Exclude []string `yaml:"exclude"`

	// Ignore lists variable names to drop from the report entirely. Entries may use glob wildcards, as in "VITE_*".
	Ignore []string `yaml:"ignore"`

	// SystemVariables ignores the built-in list. Nil means enabled.
	SystemVariables *bool `yaml:"systemVariables"`

	// Path is where this config was read from, empty when defaulted.
	Path string `yaml:"-"`
}

// Load reads the config for a scan of root.
func Load(root, explicit string) (*Config, error) {
	if explicit != "" {
		return read(explicit)
	}

	for _, name := range Names {
		path := filepath.Join(root, name)
		cfg, err := read(path)
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	return &Config{}, nil
}

func read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for _, pattern := range cfg.Ignore {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("%s: bad ignore pattern %q: %w", path, pattern, err)
		}
	}

	cfg.Path = path
	return &cfg, nil
}

// WithIgnored returns a copy with extra ignore patterns appended, which is how --ignore merges with the file.
func (c *Config) WithIgnored(patterns ...string) *Config {
	if len(patterns) == 0 {
		return c
	}

	out := *c
	out.Ignore = append(append([]string(nil), c.Ignore...), patterns...)
	return &out
}

// IgnoresVariable reports whether a variable should be dropped.
func (c *Config) IgnoresVariable(name string) bool {
	if c == nil {
		return false
	}

	if c.SystemVariables == nil || *c.SystemVariables {
		if matchAny(SystemVariables, name) {
			return true
		}
	}

	return matchAny(c.Ignore, name)
}

// matchAny reports whether name equals or globs against any pattern.
func matchAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		// An exact match is the common case and needs no glob machinery;
		// filepath.Match would also reject a name containing "[".
		if name == pattern {
			return true
		}
		if strings.ContainsAny(pattern, "*?[") {
			if ok, err := filepath.Match(pattern, name); err == nil && ok {
				return true
			}
		}
	}

	return false
}
