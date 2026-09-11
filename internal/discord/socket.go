package discord

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func FindSocket(runtimeDir string) (string, error) {
	paths, err := FindSockets(runtimeDir)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func FindSockets(runtimeDir string) ([]string, error) {
	paths := make([]string, 0, 2)
	for _, directory := range candidateDirectories(runtimeDir) {
		for index := 0; index < 10; index++ {
			path := filepath.Join(directory, "discord-ipc-"+strconv.Itoa(index))
			info, err := os.Stat(path)
			if err == nil && ownedSocket(info) {
				paths = append(paths, path)
			}
		}
	}
	return paths, nil
}

func ownedSocket(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode()&os.ModeSocket != 0
}

func candidateDirectories(runtimeDir string) []string {
	if runtimeDir != "" {
		return []string{runtimeDir}
	}
	values := make([]string, 0, 9)
	xdg := os.Getenv("XDG_RUNTIME_DIR")
	if xdg != "" {
		values = append(values,
			xdg,
			filepath.Join(xdg, "app/com.discordapp.Discord"),
			filepath.Join(xdg, "app/com.discordapp.DiscordCanary"),
			filepath.Join(xdg, "snap.discord"),
		)
	}
	values = append(values,
		"/run/user/"+strconv.Itoa(os.Getuid()),
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		os.Getenv("TEMP"),
		os.TempDir(),
	)
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
