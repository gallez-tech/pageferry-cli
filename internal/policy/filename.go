package policy

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var reservedNames = map[string]bool{
	"raw": true, "v": true, "api": true, "auth": true,
	"dashboard": true, "healthz": true,
}

func ValidateFilename(value string) (string, []string) {
	name := strings.TrimSpace(value)
	var errors []string
	if name == "" {
		errors = append(errors, "Filename is empty.")
	}
	if strings.ContainsAny(name, `/\`) {
		errors = append(errors, "Filename must not contain path separators.")
	}
	if name == "." || name == ".." {
		errors = append(errors, "Invalid filename.")
	}
	for _, r := range name {
		if r <= 0x1f || r == 0x7f {
			errors = append(errors, "Filename contains control characters.")
			break
		}
	}
	if strings.ContainsAny(name, "?#") {
		errors = append(errors, "Filename must not contain query or fragment delimiters.")
	}
	if name != "" && !strings.HasSuffix(name, ".html") && !strings.HasSuffix(name, ".htm") {
		errors = append(errors, "Filename must end with .html or .htm.")
	}
	if len(name) > 128 {
		errors = append(errors, "Filename exceeds 128 UTF-8 bytes.")
	}
	base := name
	if index := strings.LastIndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	if reservedNames[strings.ToLower(name)] || reservedNames[strings.ToLower(base)] {
		errors = append(errors, "Filename is reserved.")
	}
	if !utf8.ValidString(name) {
		errors = append(errors, "Filename is not valid UTF-8.")
	}
	return name, unique(errors)
}

func FilenameError(errors []string) error {
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("invalid public filename:\n  - %s", strings.Join(errors, "\n  - "))
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
