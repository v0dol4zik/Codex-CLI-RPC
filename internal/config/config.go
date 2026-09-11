package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

var ErrCodexExecutable = errors.New("Codex executable is unavailable")

const (
	defaultDetails = "Работаю в Codex"
	defaultState   = "Codex CLI"
)

type Settings struct {
	ClientID        string
	CodexBinary     string
	Details         string
	State           string
	LargeImage      string
	LargeText       string
	RetryInterval   time.Duration
	RefreshInterval time.Duration
	PollInterval    time.Duration
	RuntimeDir      string
	LockFile        string
}

func Path() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CODEX_RPC_CONFIG")); configured != "" {
		return expandHome(configured)
	}
	if root := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); root != "" {
		expanded, err := expandHome(root)
		if err != nil {
			return "", err
		}
		return filepath.Join(expanded, "codex-discord-rpc", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "codex-discord-rpc", "config.toml"), nil
}

func Load() (Settings, error) {
	path, err := Path()
	if err != nil {
		return Settings{}, err
	}
	values, err := loadFile(path)
	if err != nil {
		return Settings{}, err
	}

	clientID, err := textValue(values, "client_id", "CODEX_DISCORD_CLIENT_ID", "")
	if err != nil {
		return Settings{}, err
	}
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("DISCORD_CLIENT_ID"))
	}
	for _, character := range clientID {
		if character < '0' || character > '9' {
			return Settings{}, errors.New("Discord Application ID must contain digits only")
		}
	}

	configuredBinary, err := textValue(values, "codex_binary", "CODEX_BINARY", "")
	if err != nil {
		return Settings{}, err
	}
	codexBinary, err := ResolveCodexBinary(configuredBinary)
	if err != nil {
		return Settings{}, err
	}
	retry, err := durationValue(values, "retry_seconds", "CODEX_RPC_RETRY_SECONDS", 2, 0.25)
	if err != nil {
		return Settings{}, err
	}
	refresh, err := durationValue(values, "refresh_seconds", "CODEX_RPC_REFRESH_SECONDS", 15, 5)
	if err != nil {
		return Settings{}, err
	}
	poll, err := durationValue(values, "process_poll_seconds", "CODEX_RPC_PROCESS_POLL_SECONDS", 1, 0.25)
	if err != nil {
		return Settings{}, err
	}
	details, err := textValue(values, "details", "CODEX_RPC_DETAILS", defaultDetails)
	if err != nil {
		return Settings{}, err
	}
	state, err := textValue(values, "state", "CODEX_RPC_STATE", defaultState)
	if err != nil {
		return Settings{}, err
	}
	largeImage, err := textValue(values, "large_image", "CODEX_RPC_LARGE_IMAGE", "")
	if err != nil {
		return Settings{}, err
	}
	largeText, err := textValue(values, "large_text", "CODEX_RPC_LARGE_TEXT", "")
	if err != nil {
		return Settings{}, err
	}
	runtimeDir, err := textValue(values, "runtime_dir", "CODEX_RPC_RUNTIME_DIR", "")
	if err != nil {
		return Settings{}, err
	}
	lockFile, err := textValue(values, "lock_file", "CODEX_RPC_LOCK_FILE", "")
	if err != nil {
		return Settings{}, err
	}

	runtimeDir, err = expandHome(runtimeDir)
	if err != nil {
		return Settings{}, err
	}
	lockFile, err = expandHome(lockFile)
	if err != nil {
		return Settings{}, err
	}

	return Settings{
		ClientID:        clientID,
		CodexBinary:     codexBinary,
		Details:         details,
		State:           state,
		LargeImage:      largeImage,
		LargeText:       largeText,
		RetryInterval:   retry,
		RefreshInterval: refresh,
		PollInterval:    poll,
		RuntimeDir:      runtimeDir,
		LockFile:        lockFile,
	}, nil
}

func ResolveCodexBinary(configured string) (string, error) {
	if environment := strings.TrimSpace(os.Getenv("CODEX_BINARY")); environment != "" {
		configured = environment
	}
	if configured != "" {
		resolved, err := resolveExecutable(configured)
		if err != nil {
			return "", err
		}
		self, _ := os.Executable()
		if resolved == canonicalPath(self) {
			return "", fmt.Errorf("%w: path points to codex-rpc itself", ErrCodexExecutable)
		}
		return resolved, nil
	}

	self, _ := os.Executable()
	self = canonicalPath(self)
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, "codex")
		resolved, err := resolveExecutable(candidate)
		if err == nil && resolved != self {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: not found; set CODEX_BINARY", ErrCodexExecutable)
}

func resolveExecutable(value string) (string, error) {
	expanded, err := expandHome(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if !strings.ContainsRune(expanded, filepath.Separator) {
		expanded, err = exec.LookPath(expanded)
		if err != nil {
			return "", fmt.Errorf("%w: %q was not found", ErrCodexExecutable, value)
		}
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve Codex path: %w", err)
	}
	resolved := canonicalPath(absolute)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCodexExecutable, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: path is not executable: %s", ErrCodexExecutable, resolved)
	}
	return resolved, nil
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func textValue(values map[string]any, key, environment, fallback string) (string, error) {
	if value, exists := os.LookupEnv(environment); exists && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value), nil
	}
	if value, exists := values[key]; exists {
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("%s must be a string", key)
		}
		text = strings.TrimSpace(text)
		if text != "" {
			return text, nil
		}
	}
	return fallback, nil
}

func durationValue(values map[string]any, key, environment string, fallback, minimum float64) (time.Duration, error) {
	value := fallback
	if raw, exists := os.LookupEnv(environment); exists {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", key)
		}
		value = parsed
	} else if configured, exists := values[key]; exists {
		var parsed float64
		switch typed := configured.(type) {
		case float64:
			parsed = typed
		case int64:
			parsed = float64(typed)
		default:
			return 0, fmt.Errorf("%s must be a number", key)
		}
		value = parsed
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s must be finite", key)
	}
	maximum := float64(math.MaxInt64) / float64(time.Second)
	if value >= maximum {
		return 0, fmt.Errorf("%s is too large", key)
	}
	if value < minimum {
		value = minimum
	}
	return time.Duration(value * float64(time.Second)), nil
}

func loadFile(path string) (map[string]any, error) {
	values := make(map[string]any)
	if _, err := toml.DecodeFile(path, &values); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return values, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}
