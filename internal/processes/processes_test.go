package processes

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestFindMatchesReleasesAndExcludesWorkers(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "releases", "current", "bin", "codex"))
	old := executable(t, filepath.Join(root, "releases", "previous", "bin", "codex"))
	unrelated := executable(t, filepath.Join(root, "unrelated", "bin", "codex"))
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	processEntry(t, root, 4242, 1, 250, target, "codex")
	processEntry(t, root, 4243, 1, 250, unrelated, "codex")
	processEntry(t, root, 4244, 1, 350, old, "codex")
	processEntry(t, root, 4245, 1, 450, target, "codex-linux-sandbox")

	found, err := (&Scanner{ProcRoot: root}).Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].PID != 4242 || found[1].PID != 4244 {
		t.Fatalf("unexpected processes: %+v", found)
	}
	if found[0].StartTime != 1002 || found[0].Identity() != [2]uint64{4242, 250} {
		t.Fatalf("unexpected first process: %+v", found[0])
	}
}

func TestFindExcludesChildFromPreviousRelease(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "releases", "current", "bin", "codex"))
	old := executable(t, filepath.Join(root, "releases", "previous", "bin", "codex"))
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	processEntry(t, root, 4242, 1, 250, target, "codex")
	processEntry(t, root, 4243, 4242, 350, old, "codex")

	found, err := (&Scanner{ProcRoot: root}).Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].PID != 4242 {
		t.Fatalf("unexpected processes: %+v", found)
	}
}

func TestFindRecognizesDeletedExecutablePathFromSameReleaseTree(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "releases", "current", "bin", "codex"))
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	directory := filepath.Join(root, "4242")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	deleted := filepath.Join(root, "releases", "previous", "bin", "codex") + " (deleted)"
	if err := os.Symlink(deleted, filepath.Join(directory, "exe")); err != nil {
		t.Fatal(err)
	}
	fields := []any{"S", 1}
	for range 17 {
		fields = append(fields, 0)
	}
	fields = append(fields, 250)
	write(t, filepath.Join(directory, "stat"), fmt.Sprintf("4242 (codex) %s\n", join(fields)), 0o644)
	write(t, filepath.Join(directory, "cmdline"), "codex\x00", 0o644)

	found, err := (&Scanner{ProcRoot: root}).Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].PID != 4242 {
		t.Fatalf("unexpected processes: %+v", found)
	}
}

func TestFindUsesAuxvClockTicksAndSafeBootFallback(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "codex"))
	writeAuxv(t, root, 250)
	processEntry(t, root, 4242, 1, 500, target, "codex")
	now := int64(10_000)
	scanner := Scanner{ProcRoot: root, Now: func() time.Time { return time.Unix(now, 0) }}

	found, err := scanner.Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].StartTime != now {
		t.Fatalf("fallback timestamp = %+v, want discovery time %d", found, now)
	}
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	scanner = Scanner{ProcRoot: root, Now: func() time.Time { return time.Unix(now, 0) }}
	found, err = scanner.Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].StartTime != 1002 {
		t.Fatalf("auxv timestamp = %+v, want 1002", found)
	}
}

func TestFindExcludesProcessesOwnedByAnotherUser(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "codex"))
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	processEntry(t, root, 4242, 1, 100, target, "codex")
	uid := uint32(os.Geteuid() + 1)
	found, err := (&Scanner{ProcRoot: root, UID: &uid}).Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("found a process belonging to another user: %+v", found)
	}
}

func TestFindExcludesDescendantOfInternalWorker(t *testing.T) {
	root := t.TempDir()
	target := executable(t, filepath.Join(root, "codex"))
	write(t, filepath.Join(root, "stat"), "btime 1000\n", 0o644)
	processEntry(t, root, 4242, 1, 100, target, "codex")
	processEntry(t, root, 4243, 4242, 200, target, "codex-linux-sandbox")
	processEntry(t, root, 4244, 4243, 300, target, "codex")
	found, err := (&Scanner{ProcRoot: root}).Find(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].PID != 4242 {
		t.Fatalf("unexpected processes: %+v", found)
	}
}

func TestNewest(t *testing.T) {
	process, ok := Newest([]CodexProcess{{PID: 1, StartTime: 100}, {PID: 2, StartTime: 200}})
	if !ok || process.PID != 2 {
		t.Fatalf("unexpected newest process: %+v, %v", process, ok)
	}
}

func executable(t *testing.T, path string) string {
	t.Helper()
	write(t, path, "binary", 0o755)
	return path
}

func processEntry(t *testing.T, root string, pid, parent int, ticks uint64, executablePath, argv0 string) {
	t.Helper()
	directory := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executablePath, filepath.Join(directory, "exe")); err != nil {
		t.Fatal(err)
	}
	fields := []any{"S", parent}
	for range 17 {
		fields = append(fields, 0)
	}
	fields = append(fields, ticks)
	write(t, filepath.Join(directory, "stat"), fmt.Sprintf("%d (codex) %s\n", pid, join(fields)), 0o644)
	write(t, filepath.Join(directory, "cmdline"), argv0+"\x00", 0o644)
}

func join(values []any) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += " "
		}
		result += fmt.Sprint(value)
	}
	return result
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func writeAuxv(t *testing.T, root string, ticks uint64) {
	t.Helper()
	wordSize := strconv.IntSize / 8
	data := make([]byte, wordSize*4)
	if wordSize == 8 {
		binary.NativeEndian.PutUint64(data[0:8], atClockTicks)
		binary.NativeEndian.PutUint64(data[8:16], ticks)
	} else {
		binary.NativeEndian.PutUint32(data[0:4], atClockTicks)
		binary.NativeEndian.PutUint32(data[4:8], uint32(ticks))
	}
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "self", "auxv"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
