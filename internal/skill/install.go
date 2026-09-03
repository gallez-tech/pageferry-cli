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

// agents maps CLI agent names to install path segments under the project root (local)
// or the user home directory (global).
//
// OpenCode, Codex, and Cursor all discover the portable Agent Skills path
// `.agents/skills/`; Claude Code only reads `.claude/skills/`.
var agents = map[string]struct {
	local  []string
	global []string
}{
	"opencode": {[]string{".agents", "skills", "pageferry", "SKILL.md"}, []string{".agents", "skills", "pageferry", "SKILL.md"}},
	"codex":    {[]string{".agents", "skills", "pageferry", "SKILL.md"}, []string{".agents", "skills", "pageferry", "SKILL.md"}},
	"cursor":   {[]string{".agents", "skills", "pageferry", "SKILL.md"}, []string{".agents", "skills", "pageferry", "SKILL.md"}},
	"claude":   {[]string{".claude", "skills", "pageferry", "SKILL.md"}, []string{".claude", "skills", "pageferry", "SKILL.md"}},
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
	seen := make(map[string]struct{})
	var targets []string
	for _, name := range names {
		paths, ok := agents[name]
		if !ok {
			return nil, fmt.Errorf("unknown agent %q (expected opencode, codex, claude, cursor, or all)", agent)
		}
		parts := paths.local
		if global {
			parts = paths.global
		}
		target := filepath.Join(append([]string{root}, parts...)...)
		if _, dup := seen[target]; dup {
			continue
		}
		seen[target] = struct{}{}
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
