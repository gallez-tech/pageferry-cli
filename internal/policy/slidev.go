package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

const (
	WarningSlidevBundleNearLimit   = "The Slidev build is close to PageFerry's 50 MiB site bundle limit; trim Monaco language packs, disable unused features, or split content before the next upload."
	WarningSlidevMissingBundleFile = "Slidev index.html references a bundle file that is missing from the build output: %s"
	WarningSlidevSubpathBase       = "Slidev index.html loads assets from a subpath base (%s). PageFerry serves the deck at the site root; rebuild with the default base `/` (do not pass `slidev build --base`)."
	WarningSlidevNoExportedPDF     = "Slidev frontmatter has download: true but %s was not found in the build; run `slidev build --download` with playwright-chromium installed."
	WarningSlidevDrawingsPersist   = "Slidev frontmatter has drawings.persist: true; persisted drawings are stored in the browser (localStorage) and are not synchronized between devices on PageFerry."
	WarningSlidevRemotePassword    = "Slidev frontmatter defines a remote presenter password; PageFerry does not enforce that password. Protect the deck with `pageferry upload --password` or `--email` instead."
	WarningSlidevMonacoWorkers     = "Slidev Monaco blocks are present but no Monaco worker files were found under assets/; the in-slide editor may fail at runtime."
)

var slidevRootAsset = regexp.MustCompile(`(?:src|href)=["'](/[^"']+)["']`)

// IsSlidevIndex reports whether index.html looks like a Slidev production build.
func IsSlidevIndex(content []byte) bool {
	doc, err := html.Parse(bytes.NewReader(content))
	if err == nil {
		var found bool
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "meta" {
				var prop, val string
				for _, attribute := range node.Attr {
					switch attribute.Key {
					case "property":
						prop = attribute.Val
					case "content":
						val = attribute.Val
					}
				}
				if prop == "slidev:version" && val != "" {
					found = true
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(doc)
		if found {
			return true
		}
	}
	return strings.Contains(string(content), "/assets/slidev/")
}

// SlidevWarnings returns upload-time guidance for Slidev bundles.
func SlidevWarnings(site SiteResult, indexHTML []byte) []string {
	if !IsSlidevIndex(indexHTML) {
		return nil
	}
	paths := siteFileSet(site)
	var warnings []string
	if site.Bytes >= 45*1024*1024 {
		warnings = append(warnings, WarningSlidevBundleNearLimit)
	}
	for _, missing := range missingRootReferencedAssets(indexHTML, paths) {
		warnings = append(warnings, fmt.Sprintf(WarningSlidevMissingBundleFile, missing))
	}
	for _, base := range nonRootAssetBases(indexHTML) {
		warnings = append(warnings, fmt.Sprintf(WarningSlidevSubpathBase, base))
	}
	if strings.Contains(string(indexHTML), "monaco") && !hasMonacoWorkers(paths) {
		warnings = append(warnings, WarningSlidevMonacoWorkers)
	}
	return unique(warnings)
}

// SlidevSourceWarnings inspects the Slidev source next to a build (slides.md).
func SlidevSourceWarnings(projectRoot string) []string {
	content, err := os.ReadFile(filepath.Join(projectRoot, "slides.md"))
	if err != nil {
		return nil
	}
	fm := parseSlidevFrontmatter(content)
	var warnings []string
	if fm.drawingsPersist {
		warnings = append(warnings, WarningSlidevDrawingsPersist)
	}
	if fm.remotePassword {
		warnings = append(warnings, WarningSlidevRemotePassword)
	}
	if fm.downloadEnabled {
		pdfName := fm.exportPDFName()
		distPDF := filepath.Join(projectRoot, "dist", pdfName)
		if _, err := os.Stat(distPDF); err != nil {
			warnings = append(warnings, fmt.Sprintf(WarningSlidevNoExportedPDF, pdfName))
		}
	}
	return warnings
}

type slidevFrontmatter struct {
	downloadEnabled bool
	exportFilename  string
	drawingsPersist bool
	remotePassword  bool
}

// exportPDFName mirrors `slidev build`, which writes `<exportFilename>.pdf`.
func (fm slidevFrontmatter) exportPDFName() string {
	if fm.exportFilename != "" {
		return fm.exportFilename + ".pdf"
	}
	return "slidev-exported.pdf"
}

func parseSlidevFrontmatter(md []byte) slidevFrontmatter {
	body := slidevFrontmatterBody(md)
	if body == "" {
		return slidevFrontmatter{}
	}
	return slidevFrontmatter{
		downloadEnabled: frontmatterDownloadEnabled(body),
		exportFilename:  frontmatterStringValue(body, "exportFilename"),
		drawingsPersist: frontmatterDrawingsPersist(body),
		remotePassword:  frontmatterRemotePassword(body),
	}
}

func slidevFrontmatterBody(md []byte) string {
	text := string(md)
	if !strings.HasPrefix(text, "---") {
		return ""
	}
	rest := text[3:]
	if rest != "" && rest[0] == '\n' {
		rest = rest[1:]
	} else if rest != "" && rest[0] == '\r' {
		rest = rest[1:]
		if len(rest) > 0 && rest[0] == '\n' {
			rest = rest[1:]
		}
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func frontmatterDownloadEnabled(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "download:") {
			return yamlValueIsTrue(strings.TrimSpace(strings.TrimPrefix(trimmed, "download:")))
		}
	}
	return false
}

func frontmatterStringValue(body, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			return strings.Trim(val, `"'`)
		}
	}
	return ""
}

func frontmatterDrawingsPersist(body string) bool {
	lines := strings.Split(body, "\n")
	inDrawings := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "drawings:" || strings.HasPrefix(trimmed, "drawings:") {
			inDrawings = true
			if strings.Contains(trimmed, "persist:") {
				_, val, _ := strings.Cut(trimmed, "persist:")
				return yamlValueIsTrue(strings.TrimSpace(val))
			}
			continue
		}
		if inDrawings {
			if strings.HasPrefix(trimmed, "persist:") {
				return yamlValueIsTrue(strings.TrimSpace(strings.TrimPrefix(trimmed, "persist:")))
			}
			if trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				inDrawings = false
			}
		}
	}
	return false
}

func frontmatterRemotePassword(body string) bool {
	lines := strings.Split(body, "\n")
	inRemote := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "remote:") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "remote:"))
			if rest != "" {
				return !yamlValueIsFalse(rest)
			}
			inRemote = true
			continue
		}
		if inRemote {
			if strings.HasPrefix(trimmed, "password:") {
				val := strings.TrimSpace(strings.TrimPrefix(trimmed, "password:"))
				return val != "" && !yamlValueIsFalse(val)
			}
			if trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				inRemote = false
			}
		}
	}
	return false
}

func yamlValueIsTrue(value string) bool {
	value = strings.Trim(value, `"'`)
	switch strings.ToLower(value) {
	case "true", "yes", "on":
		return true
	default:
		return false
	}
}

func yamlValueIsFalse(value string) bool {
	value = strings.Trim(value, `"'`)
	switch strings.ToLower(value) {
	case "false", "no", "off", "null", "~", "":
		return true
	default:
		return false
	}
}

func siteFileSet(site SiteResult) map[string]bool {
	set := make(map[string]bool, len(site.Files))
	for _, file := range site.Files {
		set[file.Path] = true
	}
	return set
}

func missingRootReferencedAssets(indexHTML []byte, paths map[string]bool) []string {
	var missing []string
	seen := make(map[string]bool)
	doc, err := html.Parse(bytes.NewReader(indexHTML))
	if err != nil {
		return missing
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, attribute := range node.Attr {
				if attribute.Key != "src" && attribute.Key != "href" {
					continue
				}
				value := strings.TrimSpace(attribute.Val)
				if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
					continue
				}
				relative := strings.TrimPrefix(value, "/")
				if relative == "" || strings.Contains(relative, "?") {
					continue
				}
				if seen[relative] {
					continue
				}
				seen[relative] = true
				if !paths[relative] {
					missing = append(missing, relative)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	sort.Strings(missing)
	return missing
}

func nonRootAssetBases(indexHTML []byte) []string {
	var bases []string
	seen := make(map[string]bool)
	for _, match := range slidevRootAsset.FindAllStringSubmatch(string(indexHTML), -1) {
		path := match[1]
		if !strings.HasPrefix(path, "/assets/") && strings.HasPrefix(path, "/") {
			segment := strings.Trim(path, "/")
			if segment == "" {
				continue
			}
			base := "/" + strings.Split(segment, "/")[0] + "/"
			if base != "/assets/" && !seen[base] {
				seen[base] = true
				bases = append(bases, base)
			}
		}
	}
	sort.Strings(bases)
	return bases
}

func hasMonacoWorkers(paths map[string]bool) bool {
	for path := range paths {
		if strings.Contains(path, "monaco/") && strings.Contains(path, "worker") {
			return true
		}
		if strings.Contains(path, ".worker-") || strings.HasSuffix(path, "workers.js") {
			return true
		}
	}
	return false
}
