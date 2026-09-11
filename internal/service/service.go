package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	Name                    = "codex-discord-rpc.service"
	legacyWantsDirectory    = "default.target.wants"
	graphicalWantsDirectory = "graphical-session.target.wants"
	managedMarker           = "# Managed by Codex-CLI-RPC"
	managedDescription      = "Description=Discord Rich Presence monitor for Codex CLI"
	managedExecStart        = "ExecStart=%h/.local/bin/codex-rpc --monitor"
)

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type RunFunc func(arguments ...string) (Result, error)

type Manager struct {
	Directory string
	Run       RunFunc
}

func DefaultManager() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	directory := os.Getenv("CODEX_RPC_SYSTEMD_DIR")
	if directory == "" {
		directory = filepath.Join(home, ".config", "systemd", "user")
	}
	return &Manager{Directory: directory, Run: runSystemctl}, nil
}

func (manager *Manager) Path() string { return filepath.Join(manager.Directory, Name) }

// Install writes and reloads the unit. Lifecycle changes require a separate,
// explicit enable, start, stop, or restart command.
func (manager *Manager) Install() (string, error) {
	if manager.Run == nil {
		return "", errors.New("systemctl runner is not configured")
	}
	if err := manager.validateDirectory(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(manager.Directory, 0o755); err != nil {
		return "", fmt.Errorf("create systemd user directory: %w", err)
	}
	if err := manager.validateExistingUnit(); err != nil {
		return "", err
	}
	if _, err := manager.inspectManagedSymlink(graphicalWantsDirectory); err != nil {
		return "", err
	}
	if _, err := manager.inspectManagedSymlink(legacyWantsDirectory); err != nil {
		return "", err
	}
	if err := writeAtomic(manager.Path(), []byte(Unit), 0o644); err != nil {
		return "", err
	}
	if err := manager.removeLegacySymlink(); err != nil {
		return "", err
	}
	if _, err := manager.checked("daemon-reload"); err != nil {
		return "", err
	}
	return manager.Path(), nil
}

func (manager *Manager) Uninstall() (string, error) {
	if manager.Run == nil {
		return "", errors.New("systemctl runner is not configured")
	}
	if err := manager.validateDirectory(); err != nil {
		return "", err
	}
	if err := manager.validateExistingUnit(); err != nil {
		return "", err
	}
	unitExists, err := pathExists(manager.Path())
	if err != nil {
		return "", fmt.Errorf("inspect existing user service: %w", err)
	}
	graphicalLink, err := manager.inspectManagedSymlink(graphicalWantsDirectory)
	if err != nil {
		return "", err
	}
	legacyLink, err := manager.inspectManagedSymlink(legacyWantsDirectory)
	if err != nil {
		return "", err
	}
	if !unitExists && !graphicalLink && !legacyLink {
		return manager.Path(), nil
	}
	if _, err := manager.checked("disable", "--now", Name); err != nil {
		return "", fmt.Errorf("stop service before uninstalling: %w", err)
	}
	if err := manager.removeManagedSymlink(graphicalWantsDirectory); err != nil {
		return "", err
	}
	if err := manager.removeManagedSymlink(legacyWantsDirectory); err != nil {
		return "", err
	}
	if err := os.Remove(manager.Path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove user service: %w", err)
	}
	if _, err := manager.checked("daemon-reload"); err != nil {
		return "", err
	}
	return manager.Path(), nil
}

func (manager *Manager) validateDirectory() error {
	if manager.Directory == "" {
		return errors.New("systemd user directory is empty")
	}
	absolute, err := filepath.Abs(manager.Directory)
	if err != nil {
		return fmt.Errorf("resolve systemd user directory: %w", err)
	}
	absolute, err = resolvePath(absolute)
	if err != nil {
		return fmt.Errorf("resolve systemd user directory: %w", err)
	}
	if absolute == string(filepath.Separator) {
		return errors.New("refusing to use the filesystem root as the systemd user directory")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		home, _ = filepath.Abs(home)
		home = filepath.Clean(home)
		if home == absolute || strings.HasPrefix(home, absolute+string(filepath.Separator)) {
			return fmt.Errorf("refusing systemd directory that contains HOME: %s", absolute)
		}
	}
	manager.Directory = absolute
	return nil
}

func resolvePath(path string) (string, error) {
	path = filepath.Clean(path)
	missing := make([]string, 0, 4)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		path = parent
	}
}

func (manager *Manager) validateExistingUnit() error {
	info, err := os.Lstat(manager.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing user service: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace unmanaged service path: %s", manager.Path())
	}
	data, err := os.ReadFile(manager.Path())
	if err != nil {
		return fmt.Errorf("read existing user service: %w", err)
	}
	if !managedUnit(data) {
		return fmt.Errorf("refusing to replace unmanaged service file: %s", manager.Path())
	}
	return nil
}

func managedUnit(data []byte) bool {
	return bytes.Equal(data, []byte(Unit)) ||
		bytes.Equal(data, []byte(legacyGraphicalUnit)) ||
		bytes.Equal(data, []byte(legacyDefaultUnit))
}

func (manager *Manager) Action(action string) (Result, error) {
	if manager.Run == nil {
		return Result{}, errors.New("systemctl runner is not configured")
	}
	var arguments []string
	switch action {
	case "enable", "disable", "start", "stop", "restart":
		arguments = []string{action, Name}
	case "status":
		arguments = []string{"status", "--no-pager", "--full", Name}
	default:
		return Result{}, fmt.Errorf("unknown service action: %s", action)
	}
	if action != "status" {
		if err := manager.validateCurrentUnit(); err != nil {
			return Result{}, err
		}
		if _, err := manager.inspectManagedSymlink(graphicalWantsDirectory); err != nil {
			return Result{}, err
		}
		legacy, err := manager.inspectManagedSymlink(legacyWantsDirectory)
		if err != nil {
			return Result{}, err
		}
		if legacy && (action == "enable" || action == "start" || action == "restart") {
			return Result{}, errors.New("obsolete default.target enable link is present; run service install first")
		}
	}
	result, err := manager.Run(arguments...)
	if err != nil {
		return result, fmt.Errorf("run systemctl --user: %w", err)
	}
	if action != "status" && result.ExitCode != 0 {
		return result, commandError(result)
	}
	return result, nil
}

func (manager *Manager) validateCurrentUnit() error {
	if err := manager.validateDirectory(); err != nil {
		return err
	}
	info, err := os.Lstat(manager.Path())
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("current user service is not installed; run service install first")
	}
	if err != nil {
		return fmt.Errorf("inspect current user service: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("current user service is not a regular audited file; run service install first")
	}
	data, err := os.ReadFile(manager.Path())
	if err != nil {
		return fmt.Errorf("read current user service: %w", err)
	}
	if !bytes.Equal(data, []byte(Unit)) {
		return errors.New("current user service differs from the audited unit; run service install first")
	}
	return nil
}

func (manager *Manager) checked(arguments ...string) (Result, error) {
	result, err := manager.Run(arguments...)
	if err != nil {
		return result, fmt.Errorf("run systemctl --user: %w", err)
	}
	if result.ExitCode != 0 {
		return result, commandError(result)
	}
	return result, nil
}

func (manager *Manager) removeLegacySymlink() error {
	return manager.removeManagedSymlink(legacyWantsDirectory)
}

func (manager *Manager) inspectManagedSymlink(wantsDirectory string) (bool, error) {
	directory := filepath.Join(manager.Directory, wantsDirectory)
	directoryInfo, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect service enable directory: %w", err)
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return false, fmt.Errorf("refusing unmanaged service enable directory: %s", directory)
	}
	path := filepath.Join(directory, Name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect service enable link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, fmt.Errorf("refusing unmanaged non-symlink service enable path: %s", path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false, fmt.Errorf("read service enable link: %w", err)
	}
	resolvedTarget := target
	if !filepath.IsAbs(resolvedTarget) {
		resolvedTarget = filepath.Join(filepath.Dir(path), resolvedTarget)
	}
	if filepath.Clean(resolvedTarget) != manager.Path() {
		return false, fmt.Errorf("refusing unexpected service enable link: %s -> %s", path, target)
	}
	return true, nil
}

func (manager *Manager) removeManagedSymlink(wantsDirectory string) error {
	exists, err := manager.inspectManagedSymlink(wantsDirectory)
	if err != nil || !exists {
		return err
	}
	path := filepath.Join(manager.Directory, wantsDirectory, Name)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove service enable link: %w", err)
	}
	return nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary service unit: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write service unit: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync service unit: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set service unit permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close service unit: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install service unit: %w", err)
	}
	return nil
}

func runSystemctl(arguments ...string) (Result, error) {
	command := exec.Command("systemctl", append([]string{"--user"}, arguments...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	return result, err
}

func commandError(result Result) error {
	details := strings.TrimSpace(result.Stderr)
	if details == "" {
		details = strings.TrimSpace(result.Stdout)
	}
	if details == "" {
		details = fmt.Sprintf("systemctl exited with code %d", result.ExitCode)
	}
	return errors.New(details)
}
