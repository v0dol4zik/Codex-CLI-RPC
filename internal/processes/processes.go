package processes

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultClockTicks = 100
	atClockTicks      = 17
)

type CodexProcess struct {
	PID        int
	StartTime  int64
	StartTicks uint64
	Executable string
}

func (process CodexProcess) Identity() [2]uint64 {
	return [2]uint64{uint64(process.PID), process.StartTicks}
}

type Scanner struct {
	ProcRoot string
	Now      func() time.Time
	UID      *uint32

	timingOnce sync.Once
	boot       int64
	hasBoot    bool
	ticks      uint64
}

func (scanner *Scanner) Find(codexBinary string) ([]CodexProcess, error) {
	root := scanner.ProcRoot
	if root == "" {
		root = "/proc"
	}
	target := canonicalPath(codexBinary)
	scanner.timingOnce.Do(func() {
		scanner.boot, scanner.hasBoot = scanner.bootTime(root)
		scanner.ticks = clockTicks(root)
	})
	scanTime := scanner.now().Unix()
	uid := uint32(os.Geteuid())
	if scanner.UID != nil {
		uid = *scanner.UID
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	type match struct {
		process CodexProcess
		parent  int
		worker  bool
	}
	matches := make([]match, 0, 4)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		pidDirectory := filepath.Join(root, entry.Name())
		executable, err := processExecutable(filepath.Join(pidDirectory, "exe"))
		if err != nil || !matchesExecutable(executable, target) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !ownedBy(info, uid) {
			continue
		}
		parent, ticks, err := readStat(filepath.Join(pidDirectory, "stat"))
		if err != nil {
			continue
		}
		startTime := scanTime
		if scanner.hasBoot {
			startTime = scanner.boot + int64(ticks/scanner.ticks)
		}
		matches = append(matches, match{
			process: CodexProcess{
				PID:        pid,
				StartTime:  startTime,
				StartTicks: ticks,
				Executable: executable,
			},
			parent: parent,
			worker: internalWorker(pidDirectory),
		})
	}

	matchedPIDs := make(map[int]struct{}, len(matches))
	for _, candidate := range matches {
		matchedPIDs[candidate.process.PID] = struct{}{}
	}
	result := make([]CodexProcess, 0, len(matches))
	for _, candidate := range matches {
		if candidate.worker {
			continue
		}
		if _, child := matchedPIDs[candidate.parent]; !child {
			result = append(result, candidate.process)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return less(result[left], result[right])
	})
	return result, nil
}

func ownedBy(info os.FileInfo, uid uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uid
}

func processExecutable(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	target = strings.TrimSuffix(target, " (deleted)")
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return canonicalPath(target), nil
}

func Newest(processes []CodexProcess) (CodexProcess, bool) {
	if len(processes) == 0 {
		return CodexProcess{}, false
	}
	newest := processes[0]
	for _, process := range processes[1:] {
		if less(newest, process) {
			newest = process
		}
	}
	return newest, true
}

func less(left, right CodexProcess) bool {
	if left.StartTime != right.StartTime {
		return left.StartTime < right.StartTime
	}
	if left.StartTicks != right.StartTicks {
		return left.StartTicks < right.StartTicks
	}
	return left.PID < right.PID
}

func (scanner *Scanner) bootTime(root string) (int64, bool) {
	data, err := os.ReadFile(filepath.Join(root, "stat"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "btime ") {
				value, parseErr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
				if parseErr == nil {
					return value, true
				}
			}
		}
	}
	data, err = os.ReadFile(filepath.Join(root, "uptime"))
	if err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			uptime, parseErr := strconv.ParseFloat(fields[0], 64)
			if parseErr == nil {
				return scanner.now().Unix() - int64(uptime), true
			}
		}
	}
	return 0, false
}

func clockTicks(root string) uint64 {
	data, err := os.ReadFile(filepath.Join(root, "self", "auxv"))
	if err != nil {
		return defaultClockTicks
	}
	wordSize := strconv.IntSize / 8
	entrySize := wordSize * 2
	for offset := 0; offset+entrySize <= len(data); offset += entrySize {
		key := nativeUint(data[offset : offset+wordSize])
		value := nativeUint(data[offset+wordSize : offset+entrySize])
		if key == atClockTicks && value > 0 {
			return value
		}
		if key == 0 {
			break
		}
	}
	return defaultClockTicks
}

func nativeUint(data []byte) uint64 {
	if len(data) == 4 {
		return uint64(binary.NativeEndian.Uint32(data))
	}
	return binary.NativeEndian.Uint64(data)
}

func (scanner *Scanner) now() time.Time {
	if scanner.Now != nil {
		return scanner.Now()
	}
	return time.Now()
}

func readStat(path string) (int, uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	closing := bytes.LastIndex(data, []byte(") "))
	if closing < 0 {
		return 0, 0, errors.New("process stat has no command terminator")
	}
	fields := strings.Fields(string(data[closing+2:]))
	if len(fields) <= 19 {
		return 0, 0, errors.New("process stat has no start time")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, err
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return parent, ticks, nil
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(path)
}

func standaloneReleaseRoot(executable string) (string, bool) {
	if filepath.Base(executable) != "codex" || filepath.Base(filepath.Dir(executable)) != "bin" {
		return "", false
	}
	releaseRoot := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if filepath.Base(releaseRoot) != "releases" {
		return "", false
	}
	return releaseRoot, true
}

func matchesExecutable(executable, target string) bool {
	if executable == target {
		return true
	}
	targetRoot, ok := standaloneReleaseRoot(target)
	if !ok {
		return false
	}
	executableRoot, ok := standaloneReleaseRoot(executable)
	return ok && executableRoot == targetRoot
}

func internalWorker(pidDirectory string) bool {
	data, err := os.ReadFile(filepath.Join(pidDirectory, "cmdline"))
	if err != nil {
		return false
	}
	if separator := bytes.IndexByte(data, 0); separator >= 0 {
		data = data[:separator]
	}
	return filepath.Base(string(data)) == "codex-linux-sandbox"
}
