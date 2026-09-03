package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLocalAndGlobal(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	cwd, home := filepath.Join(t.TempDir(), "project"), t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := Install("all", false, false, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("installed %d paths", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "name: pageferry") {
			t.Fatalf("invalid skill at %s: %v", path, err)
		}
	}
	paths, err = Install("opencode", true, false, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "opencode", "skills", "pageferry", "SKILL.md")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("paths = %v, want %s", paths, want)
	}
}

func TestInstallHonorsCodexHome(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "custom-codex")
	t.Setenv("CODEX_HOME", codexHome)
	paths, err := Install("codex", true, false, root, filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(codexHome, "skills", "pageferry", "SKILL.md")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("paths = %v, want %s", paths, want)
	}
}

func TestInstallRefusesOverwriteWithoutForce(t *testing.T) {
	root := t.TempDir()
	if _, err := Install("codex", false, false, root, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("codex", false, false, root, root); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected overwrite error, got %v", err)
	}
	if _, err := Install("codex", false, true, root, root); err != nil {
		t.Fatalf("forced install: %v", err)
	}
}
