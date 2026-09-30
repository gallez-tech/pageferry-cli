package policy

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaxSiteBytes     = 50 * 1024 * 1024
	MaxSiteFiles     = 2000
	SiteIndex        = "index.html"
	maxSitePathBytes = 512
)

// SiteFile is one file of a static site bundle, such as the dist/ directory
// produced by `slidev build` or any Vite build.
type SiteFile struct {
	Path     string // bundle-relative, slash-separated
	Absolute string
	Size     int64
}

type SiteResult struct {
	Root     string
	Files    []SiteFile
	Bytes    int64
	Title    string
	Skipped  []string
	Warnings []string
	Errors   []string
}

// ValidateSitePath mirrors the server's bundle path policy.
func ValidateSitePath(value string) string {
	if value == "" {
		return "File path is empty."
	}
	if len(value) > maxSitePathBytes {
		return fmt.Sprintf("File path exceeds %d UTF-8 bytes: %s", maxSitePathBytes, value)
	}
	if !utf8.ValidString(value) {
		return fmt.Sprintf("File path is not valid UTF-8: %q", value)
	}
	for _, r := range value {
		if r <= 0x1f || r == 0x7f || r == '\\' || r == '?' || r == '#' {
			return fmt.Sprintf("File path contains forbidden characters: %s", value)
		}
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Sprintf("File path must be relative: %s", value)
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Sprintf("Invalid file path: %s", value)
		}
		if strings.HasPrefix(segment, ".") {
			return fmt.Sprintf("Hidden files are not allowed: %s", value)
		}
	}
	if value == "__pageferry" || strings.HasPrefix(value, "__pageferry/") {
		return fmt.Sprintf("File path is reserved: %s", value)
	}
	return ""
}

// CollectSite walks a build output directory. Hidden files and directories
// (.DS_Store, .git, …) and symbolic links are skipped rather than rejected.
func CollectSite(root string) (SiteResult, error) {
	result := SiteResult{Root: root}
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(entry.Name(), ".") {
			result.Skipped = append(result.Skipped, relative)
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			result.Skipped = append(result.Skipped, relative)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if message := ValidateSitePath(relative); message != "" {
			result.Errors = append(result.Errors, message)
			return nil
		}
		result.Files = append(result.Files, SiteFile{Path: relative, Absolute: current, Size: info.Size()})
		result.Bytes += info.Size()
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("read %s: %w", root, err)
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })

	if len(result.Files) > MaxSiteFiles {
		result.Errors = append(result.Errors, fmt.Sprintf("Site bundle has %d files; maximum is %d.", len(result.Files), MaxSiteFiles))
	}
	if result.Bytes > MaxSiteBytes {
		result.Errors = append(result.Errors, fmt.Sprintf("Site bundle is %d bytes; maximum is %d bytes.", result.Bytes, MaxSiteBytes))
	}
	hasIndex := false
	for _, file := range result.Files {
		if file.Path == SiteIndex {
			hasIndex = true
		}
		extension := strings.ToLower(path.Ext(file.Path))
		if extension != ".html" && extension != ".htm" {
			continue
		}
		content, err := os.ReadFile(file.Absolute)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", file.Absolute, err)
		}
		validation := ValidateHTMLWithOptions(content, HTMLOptions{AllowRelativeScripts: true})
		for _, message := range validation.Errors {
			result.Errors = append(result.Errors, file.Path+": "+message)
		}
		if file.Path == SiteIndex {
			result.Title = validation.Title
			result.Warnings = append(result.Warnings, validation.Warnings...)
		}
	}
	if !hasIndex {
		result.Errors = append(result.Errors, "Site bundle must contain index.html at its root.")
	}
	result.Errors = unique(result.Errors)
	return result, nil
}
