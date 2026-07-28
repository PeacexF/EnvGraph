package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PeacexF/EnvGraph/internal/config"
)

// write puts a config file in a fresh directory and returns the directory.
func write(t *testing.T, name, content string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func load(t *testing.T, root, explicit string) *config.Config {
	t.Helper()

	cfg, err := config.Load(root, explicit)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func TestMissingConfigIsNotAnError(t *testing.T) {
	cfg := load(t, t.TempDir(), "")

	if len(cfg.Exclude) != 0 || len(cfg.Ignore) != 0 {
		t.Errorf("config = %+v, want empty", cfg)
	}
	if cfg.Path != "" {
		t.Errorf("path = %q, want empty when defaulted", cfg.Path)
	}
}

func TestLoadsFromTheRoot(t *testing.T) {
	for _, name := range config.Names {
		t.Run(name, func(t *testing.T) {
			dir := write(t, name, "exclude:\n  - examples\nignore:\n  - OLD_KEY\n")
			cfg := load(t, dir, "")

			if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "examples" {
				t.Errorf("exclude = %v, want [examples]", cfg.Exclude)
			}
			if len(cfg.Ignore) != 1 || cfg.Ignore[0] != "OLD_KEY" {
				t.Errorf("ignore = %v, want [OLD_KEY]", cfg.Ignore)
			}
			if cfg.Path == "" {
				t.Error("path is empty, want the file it was read from")
			}
		})
	}
}

func TestExplicitPath(t *testing.T) {
	dir := write(t, "custom.yml", "ignore:\n  - FROM_CUSTOM\n")
	cfg := load(t, t.TempDir(), filepath.Join(dir, "custom.yml"))

	if !cfg.IgnoresVariable("FROM_CUSTOM") {
		t.Errorf("config = %+v, want the explicit file to be used", cfg)
	}
}

func TestExplicitPathMustExist(t *testing.T) {
	// A missing file is fine when defaulted, but naming one that is not there is a mistake worth reporting.
	if _, err := config.Load(t.TempDir(), filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Error("Load() error = nil, want an error for a named file that does not exist")
	}
}

func TestMalformedYAML(t *testing.T) {
	dir := write(t, ".envgraph.yml", "exclude: [unclosed\n")

	if _, err := config.Load(dir, ""); err == nil {
		t.Error("Load() error = nil, want an error for malformed yaml")
	}
}

func TestExactIgnore(t *testing.T) {
	dir := write(t, ".envgraph.yml", "ignore:\n  - OLD_KEY\n")
	cfg := load(t, dir, "")

	if !cfg.IgnoresVariable("OLD_KEY") {
		t.Error("OLD_KEY should be ignored")
	}
	if cfg.IgnoresVariable("OLD_KEY_2") {
		t.Error("OLD_KEY_2 should not be ignored by an exact rule")
	}
}

func TestGlobIgnore(t *testing.T) {
	dir := write(t, ".envgraph.yml", "ignore:\n  - \"VITE_*\"\n  - \"*_SECRET\"\n  - \"AWS_?\"\n")
	cfg := load(t, dir, "")

	for _, name := range []string{"VITE_API_URL", "VITE_", "SESSION_SECRET", "AWS_A"} {
		if !cfg.IgnoresVariable(name) {
			t.Errorf("%s should be ignored", name)
		}
	}
	for _, name := range []string{"API_URL", "SECRET_KEY", "AWS_LONG"} {
		if cfg.IgnoresVariable(name) {
			t.Errorf("%s should not be ignored", name)
		}
	}
}

func TestMalformedGlobIsReported(t *testing.T) {
	// A variable name can never contain "[" — every parser enforces
	// [A-Za-z_][A-Za-z0-9_]* — so an unclosed bracket is a typo in the
	// pattern, and saying so beats silently matching nothing.
	dir := write(t, ".envgraph.yml", "ignore:\n  - \"UNCLOSED[\"\n")

	if _, err := config.Load(dir, ""); err == nil {
		t.Error("Load() error = nil, want an error naming the bad pattern")
	}
}

func TestSystemVariablesIgnoredByDefault(t *testing.T) {
	cfg := load(t, t.TempDir(), "")

	for _, name := range config.SystemVariables {
		if !cfg.IgnoresVariable(name) {
			t.Errorf("%s should be ignored by default", name)
		}
	}
	if cfg.IgnoresVariable("DATABASE_URL") {
		t.Error("a project variable should not be ignored")
	}
}

func TestSystemVariablesCanBeTurnedOff(t *testing.T) {
	dir := write(t, ".envgraph.yml", "systemVariables: false\n")
	cfg := load(t, dir, "")

	if cfg.IgnoresVariable("PATH") {
		t.Error("PATH should be reported when systemVariables is false")
	}
}

func TestSystemVariablesExplicitlyOn(t *testing.T) {
	dir := write(t, ".envgraph.yml", "systemVariables: true\n")

	if !load(t, dir, "").IgnoresVariable("PATH") {
		t.Error("PATH should be ignored when systemVariables is true")
	}
}

func TestWithIgnored(t *testing.T) {
	dir := write(t, ".envgraph.yml", "ignore:\n  - FROM_FILE\n")
	cfg := load(t, dir, "").WithIgnored("FROM_FLAG", "GLOB_*")

	for _, name := range []string{"FROM_FILE", "FROM_FLAG", "GLOB_X"} {
		if !cfg.IgnoresVariable(name) {
			t.Errorf("%s should be ignored", name)
		}
	}
}

func TestWithIgnoredDoesNotMutateTheOriginal(t *testing.T) {
	dir := write(t, ".envgraph.yml", "ignore:\n  - FROM_FILE\n")
	cfg := load(t, dir, "")
	cfg.WithIgnored("FROM_FLAG")

	if cfg.IgnoresVariable("FROM_FLAG") {
		t.Error("WithIgnored changed the config it was called on")
	}
}

func TestWithIgnoredWithNothingToAdd(t *testing.T) {
	cfg := load(t, t.TempDir(), "")

	if got := cfg.WithIgnored(); got != cfg {
		t.Error("WithIgnored() with no patterns should return the same config")
	}
}

func TestNilConfigIgnoresNothing(t *testing.T) {
	var cfg *config.Config

	if cfg.IgnoresVariable("PATH") {
		t.Error("a nil config should ignore nothing")
	}
}

func TestEmptyFile(t *testing.T) {
	dir := write(t, ".envgraph.yml", "")
	cfg := load(t, dir, "")

	if !cfg.IgnoresVariable("PATH") {
		t.Error("an empty file should still leave the system defaults on")
	}
}
