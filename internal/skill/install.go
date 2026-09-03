package skill

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed pageferry/SKILL.md
var content []byte

var agents = map[string]struct {
	local  []string
	global []string
}{
	"opencode": {[]string{".opencode", "skills", "pageferry", "SKILL.md"}, []string{".config", "opencode", "skills", "pageferry", "SKILL.md"}},
	"codex":    {[]string{".codex", "skills", "pageferry", "SKILL.md"}, []string{".codex", "skills", "pageferry", "SKILL.md"}},
	"claude":   {[]string{".claude", "skills", "pageferry", "SKILL.md"}, []string{".claude", "skills", "pageferry", "SKILL.md"}},
	"cursor":   {[]string{".cursor", "skills", "pageferry", "SKILL.md"}, []string{".cursor", "skills", "pageferry", "SKILL.md"}},
}

func Names() []string { return []string{"opencode", "codex", "claude", "cursor"} }

func Install(agent string, global, force bool, cwd, home string) ([]string, error) {
	names := []string{strings.ToLower(agent)}
	if names[0] == "all" {
		names = Names()
	}
	root := cwd
	if global {
		root = home
	} else {
		root = projectRoot(cwd)
	}
	var targets []string
	for _, name := range names {
		paths, ok := agents[name]
		if !ok {
			return nil, fmt.Errorf("unknown agent %q (expected opencode, codex, claude, cursor, or all)", agent)
		}
		targetRoot := root
		parts := paths.local
		if global {
			parts = paths.global
			if name == "codex" {
				if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
					targetRoot = codexHome
					parts = []string{"skills", "pageferry", "SKILL.md"}
				}
			}
		}
		target := filepath.Join(append([]string{targetRoot}, parts...)...)
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing to replace symbolic link %s", target)
		} else if err == nil && !force {
			return nil, fmt.Errorf("%s already exists; use --force to replace it", target)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		targets = append(targets, target)
	}
	var installed []string
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return nil, err
		}
		if err := os.Chmod(target, 0o644); err != nil {
			return nil, err
		}
		installed = append(installed, target)
	}
	return installed, nil
}

func projectRoot(start string) string {
	current, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}
