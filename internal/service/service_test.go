package service

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type callLog struct {
	calls [][]string
}

func (log *callLog) run(arguments ...string) (Result, error) {
	log.calls = append(log.calls, append([]string(nil), arguments...))
	return Result{}, nil
}

func TestUnitMatchesRepositoryAndDoesNotPullGraphicalTarget(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "systemd", Name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Unit {
		t.Fatal("embedded unit differs from systemd/codex-discord-rpc.service")
	}
	for _, forbidden := range []string{"Wants=graphical-session.target", "Requires=graphical-session.target", "WantedBy=default.target"} {
		if strings.Contains(Unit, forbidden) {
			t.Fatalf("unsafe unit dependency found: %s", forbidden)
		}
	}
	for _, required := range []string{
		"PartOf=graphical-session.target",
		"WantedBy=graphical-session.target",
		"Type=exec",
		"Restart=on-failure",
		"RestartPreventExitStatus=2 75 127 203",
		"StartLimitBurst=5",
		"MemoryMax=64M",
		"TasksMax=64",
		"NoNewPrivileges=yes",
		"RestrictAddressFamilies=AF_UNIX",
	} {
		if !strings.Contains(Unit, required) {
			t.Fatalf("required unit setting missing: %s", required)
		}
	}
}

func TestInstallOnlyReloadsAndRemovesLegacySymlink(t *testing.T) {
	directory := t.TempDir()
	legacy := filepath.Join(directory, legacyWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, Name), legacy); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	path, err := manager.Install()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy symlink was not removed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Unit {
		t.Fatal("installed unit content differs")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("unit mode = %o", info.Mode().Perm())
	}
	want := [][]string{{"daemon-reload"}}
	if !reflect.DeepEqual(log.calls, want) {
		t.Fatalf("install invoked unexpected lifecycle commands: %#v", log.calls)
	}
}

func TestInstallRefusesNonSymlinkLegacyPathBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	legacy := filepath.Join(directory, legacyWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for a non-symlink legacy path")
	}
	if _, err := os.Stat(manager.Path()); !os.IsNotExist(err) {
		t.Fatal("unit was written before legacy-path validation")
	}
	data, err := os.ReadFile(legacy)
	if err != nil || string(data) != "user data" {
		t.Fatalf("legacy path was modified: %q, %v", data, err)
	}
}

func TestInstallRefusesUnexpectedLegacySymlink(t *testing.T) {
	directory := t.TempDir()
	legacy := filepath.Join(directory, legacyWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "unrelated.service"), legacy); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for unexpected legacy symlink")
	}
	if _, err := os.Lstat(legacy); err != nil {
		t.Fatalf("unexpected legacy symlink was modified: %v", err)
	}
}

func TestInstallRefusesUnmanagedUnit(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for unmanaged unit")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "[Unit]\nDescription=unrelated\n" {
		t.Fatalf("unmanaged unit was modified: %q, %v", data, err)
	}
}

func TestInstallRefusesSpoofedDescriptionAndExecStart(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	data := "[Unit]\n" + managedDescription + "\n[Service]\n" + managedExecStart + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for a unit without a managed marker or legacy signature")
	}
}

func TestInstallRefusesMarkerWithUnrelatedUnitBody(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	data := managedMarker + "\n[Unit]\n" + managedDescription + "\n[Service]\n" + managedExecStart + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for a marked but non-repository unit")
	}
}

func TestInstallAcceptsLegacyManagedUnit(t *testing.T) {
	for name, legacyUnit := range map[string]string{
		"v0.1.0 default target":   legacyDefaultUnit,
		"v0.1.1 graphical target": legacyGraphicalUnit,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, Name)
			if err := os.WriteFile(path, []byte(legacyUnit), 0o644); err != nil {
				t.Fatal(err)
			}
			manager := Manager{Directory: directory, Run: (&callLog{}).run}
			if _, err := manager.Install(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != Unit {
				t.Fatalf("legacy unit was not upgraded: %v", err)
			}
		})
	}
}

func TestInstallRefusesModifiedLegacyUnit(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	modified := legacyGraphicalUnit + "ExecStartPost=/bin/false\n"
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for a modified legacy unit")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != modified {
		t.Fatalf("modified legacy unit was changed: %v", err)
	}
}

func TestInstallRefusesUnsafeDirectory(t *testing.T) {
	manager := Manager{Directory: string(filepath.Separator), Run: (&callLog{}).run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for filesystem root")
	}
}

func TestInstallRefusesDirectorySymlinkedToRoot(t *testing.T) {
	link := filepath.Join(t.TempDir(), "systemd-link")
	if err := os.Symlink(string(filepath.Separator), link); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: link, Run: log.run}
	if _, err := manager.Install(); err == nil {
		t.Fatal("expected refusal for directory symlinked to root")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called: %#v", log.calls)
	}
}

func TestLifecycleActionsAreExplicit(t *testing.T) {
	log := &callLog{}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, Name), []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Action("enable"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Action("start"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"enable", Name}, {"start", Name}}
	if !reflect.DeepEqual(log.calls, want) {
		t.Fatalf("unexpected lifecycle calls: %#v", log.calls)
	}
}

func TestStartActionsRequireCurrentSafeUnit(t *testing.T) {
	for name, unit := range map[string]string{
		"missing": "",
		"legacy": `[Unit]
Description=Discord Rich Presence monitor for Codex CLI
After=graphical-session.target
PartOf=graphical-session.target
[Service]
Type=simple
ExecStart=%h/.local/bin/codex-rpc --monitor
Restart=always
[Install]
WantedBy=graphical-session.target
`,
		"unsafe": `# Managed by Codex-CLI-RPC
[Unit]
Description=Discord Rich Presence monitor for Codex CLI
Wants=graphical-session.target
PartOf=graphical-session.target
[Service]
ExecStart=%h/.local/bin/codex-rpc --monitor
[Install]
WantedBy=graphical-session.target
`,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if unit != "" {
				if err := os.WriteFile(filepath.Join(directory, Name), []byte(unit), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			log := &callLog{}
			manager := Manager{Directory: directory, Run: log.run}
			for _, action := range []string{"enable", "disable", "start", "stop", "restart"} {
				if _, err := manager.Action(action); err == nil {
					t.Fatalf("%s accepted %s unit", action, name)
				}
			}
			if len(log.calls) != 0 {
				t.Fatalf("systemctl was called: %#v", log.calls)
			}
		})
	}
}

func TestMutationRefusesSymlinkedUnit(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "audited.service")
	if err := os.WriteFile(target, []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, Name)); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Action("start"); err == nil {
		t.Fatal("start accepted a symlinked unit")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called: %#v", log.calls)
	}
}

func TestStartRefusesObsoleteDefaultTargetLink(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, Name), []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(directory, legacyWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", Name), legacy); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Action("start"); err == nil {
		t.Fatal("start accepted obsolete default.target link")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called: %#v", log.calls)
	}
}

func TestStartRefusesUnexpectedGraphicalTargetLink(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, Name), []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, graphicalWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "unrelated.service"), link); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Action("start"); err == nil {
		t.Fatal("start accepted an unexpected graphical target link")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called: %#v", log.calls)
	}
}

func TestStartRefusesSymlinkedEnableDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, Name), []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	wants := filepath.Join(directory, graphicalWantsDirectory)
	if err := os.Symlink(t.TempDir(), wants); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Action("start"); err == nil {
		t.Fatal("start accepted a symlinked enable directory")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called: %#v", log.calls)
	}
}

func TestUninstallStopsBeforeRemoving(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	if err := os.WriteFile(path, []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unit was not removed")
	}
	want := [][]string{{"disable", "--now", Name}, {"daemon-reload"}}
	if !reflect.DeepEqual(log.calls, want) {
		t.Fatalf("unexpected uninstall calls: %#v", log.calls)
	}
}

func TestUninstallIsIdempotentWhenNothingIsInstalled(t *testing.T) {
	log := &callLog{}
	manager := Manager{Directory: t.TempDir(), Run: log.run}
	if _, err := manager.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called for an absent unit: %#v", log.calls)
	}
}

func TestUninstallRemovesKnownEnableLinksOnly(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, Name)
	if err := os.WriteFile(path, []byte(Unit), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, wants := range []string{graphicalWantsDirectory, legacyWantsDirectory} {
		link := filepath.Join(directory, wants, Name)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", Name), link); err != nil {
			t.Fatal(err)
		}
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Uninstall(); err != nil {
		t.Fatal(err)
	}
	for _, wants := range []string{graphicalWantsDirectory, legacyWantsDirectory} {
		if _, err := os.Lstat(filepath.Join(directory, wants, Name)); !os.IsNotExist(err) {
			t.Fatalf("enable link still exists in %s: %v", wants, err)
		}
	}
}

func TestUninstallRefusesUnexpectedEnableLink(t *testing.T) {
	directory := t.TempDir()
	link := filepath.Join(directory, graphicalWantsDirectory, Name)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "unrelated.service"), link); err != nil {
		t.Fatal(err)
	}
	log := &callLog{}
	manager := Manager{Directory: directory, Run: log.run}
	if _, err := manager.Uninstall(); err == nil {
		t.Fatal("expected refusal for an unexpected enable link")
	}
	if len(log.calls) != 0 {
		t.Fatalf("systemctl was called before validation: %#v", log.calls)
	}
}
