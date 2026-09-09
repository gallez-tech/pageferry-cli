package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLocalAndGlobal(t *testing.T) {
	cwd, home := filepath.Join(t.TempDir(), "project"), t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := Install("all", false, false, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	wantLocal := []string{
		filepath.Join(cwd, ".agents", "skills", "pageferry", "SKILL.md"),
		filepath.Join(cwd, ".claude", "skills", "pageferry", "SKILL.md"),
	}
	if len(paths) != len(wantLocal) {
		t.Fatalf("installed %d paths, want %d: %v", len(paths), len(wantLocal), paths)
	}
	for i, want := range wantLocal {
		if paths[i].Path != want || paths[i].Status != Installed {
			t.Fatalf("results[%d] = %#v, want installed %s", i, paths[i], want)
		}
		data, err := os.ReadFile(paths[i].Path)
		if err != nil {
			t.Fatalf("invalid skill at %s: %v", paths[i].Path, err)
		}
		content := string(data)
		for _, required := range []string{"name: pageferry", "Alpine.js", "HTMX", "public unless uploaded", "--secret NAME=value", "Pin dependency versions", "Subresource Integrity", "pageferry validate <file-path>", "pageferry upload <file-path>"} {
			if !strings.Contains(content, required) {
				t.Errorf("skill at %s is missing %q", paths[i].Path, required)
			}
		}
	}

	wantAgents := filepath.Join(home, ".agents", "skills", "pageferry", "SKILL.md")
	paths, err = Install("opencode", true, false, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].Path != wantAgents {
		t.Fatalf("opencode global paths = %v, want %s", paths, wantAgents)
	}
	for _, agent := range []string{"codex", "cursor"} {
		paths, err = Install(agent, true, true, cwd, home)
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) != 1 || paths[0].Path != wantAgents {
			t.Fatalf("%s global paths = %v, want %s", agent, paths, wantAgents)
		}
	}

	paths, err = Install("claude", true, false, cwd, home)
	if err != nil {
		t.Fatal(err)
	}
	wantClaude := filepath.Join(home, ".claude", "skills", "pageferry", "SKILL.md")
	if len(paths) != 1 || paths[0].Path != wantClaude {
		t.Fatalf("claude global paths = %v, want %s", paths, wantClaude)
	}
}

func TestInstallRefusesOverwriteWithoutForce(t *testing.T) {
	root := t.TempDir()
	if _, err := Install("codex", false, false, root, root); err != nil {
		t.Fatal(err)
	}
	results, err := Install("codex", false, false, root, root)
	if err != nil || len(results) != 1 || results[0].Status != Current {
		t.Fatalf("current install = %#v, %v", results, err)
	}
	if err := os.WriteFile(results[0].Path, []byte("old skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("codex", false, false, root, root); err == nil || !strings.Contains(err.Error(), "outdated") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected outdated skill error, got %v", err)
	}
	results, err = Install("codex", false, true, root, root)
	if err != nil {
		t.Fatalf("forced install: %v", err)
	}
	if len(results) != 1 || results[0].Status != Updated {
		t.Fatalf("forced install = %#v, want updated", results)
	}
}

func TestInstallAllDedupesAgentsPath(t *testing.T) {
	root := t.TempDir()
	if _, err := Install("cursor", false, false, root, root); err != nil {
		t.Fatal(err)
	}
	// cursor already wrote the current .agents skill; --agent all recognizes
	// that copy and installs only the missing Claude skill.
	results, err := Install("all", false, false, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Status != Current || results[1].Status != Installed {
		t.Fatalf("all install results = %#v", results)
	}
	paths, err := Install("all", false, true, root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("forced all installed %d paths, want 2: %v", len(paths), paths)
	}
}
