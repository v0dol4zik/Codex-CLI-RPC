package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigAndEnvironmentPrecedence(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.toml")
	data := "client_id = \"123456789\"\n" +
		"codex_binary = \"" + binary + "\"\n" +
		"details = \"from # config\" # comment\n" +
		"retry_seconds = 0.1\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_RPC_CONFIG", path)
	t.Setenv("CODEX_RPC_DETAILS", "from environment")
	t.Setenv("CODEX_BINARY", "")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClientID != "123456789" || settings.Details != "from environment" {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	if settings.RetryInterval != 250*time.Millisecond {
		t.Fatalf("retry interval = %s", settings.RetryInterval)
	}
}

func TestMalformedConfigIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("invalid = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_RPC_CONFIG", path)
	t.Setenv("CODEX_BINARY", "/bin/true")
	if _, err := Load(); err == nil {
		t.Fatal("expected malformed config error")
	}
}

func TestMissingConfigUsesDefaults(t *testing.T) {
	t.Setenv("CODEX_RPC_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("CODEX_BINARY", "/bin/true")
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Details != defaultDetails || settings.State != defaultState {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
}

func TestIntegerAndFloatDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := "retry_seconds = 1\nrefresh_seconds = 5.5\nprocess_poll_seconds = 2\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_RPC_CONFIG", path)
	t.Setenv("CODEX_BINARY", "/bin/true")
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.RetryInterval != time.Second || settings.RefreshInterval != 5500*time.Millisecond || settings.PollInterval != 2*time.Second {
		t.Fatalf("unexpected durations: %+v", settings)
	}
}

func TestQuotedCommentsAndLiteralStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := "details = \"work #1\" # real comment\nstate = 'Codex CLI'\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_RPC_CONFIG", path)
	t.Setenv("CODEX_BINARY", "/bin/true")
	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Details != "work #1" || settings.State != "Codex CLI" {
		t.Fatalf("unexpected strings: %+v", settings)
	}
}

func TestDuplicateAndNonFiniteNumbersAreRejected(t *testing.T) {
	for name, contents := range map[string]string{
		"duplicate": "retry_seconds = 1\nretry_seconds = 2\n",
		"nan":       "retry_seconds = nan\n",
		"infinity":  "retry_seconds = inf\n",
		"overflow":  "retry_seconds = 1e100\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEX_RPC_CONFIG", path)
			t.Setenv("CODEX_BINARY", "/bin/true")
			if _, err := Load(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestTrailingTextAndWrongTypesAreRejected(t *testing.T) {
	for name, contents := range map[string]string{
		"trailing":     "retry_seconds = 1 second\n",
		"wrong_type":   "details = 123\n",
		"unterminated": "details = \"broken\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEX_RPC_CONFIG", path)
			t.Setenv("CODEX_BINARY", "/bin/true")
			if _, err := Load(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestResolveCodexBinarySkipsSelfSymlink(t *testing.T) {
	directory := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(directory, "first")
	second := filepath.Join(directory, "second")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(first, "codex")); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(second, "codex")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_BINARY", "")
	t.Setenv("PATH", first+string(os.PathListSeparator)+second)
	resolved, err := ResolveCodexBinary("")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != real {
		t.Fatalf("resolved %q, want %q", resolved, real)
	}
}
