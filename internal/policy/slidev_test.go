package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectSiteIncludesSlidevFixtureFiles(t *testing.T) {
	root := filepath.Join("testdata", "slidev-dist")
	site, err := CollectSite(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(site.Errors) != 0 {
		t.Fatalf("errors = %v", site.Errors)
	}
	want := []string{
		"index.html",
		"404.html",
		"_redirects",
		"slidev-exported.pdf",
		"assets/index-nI7gVUI6.js",
		"assets/editor.worker-BbVB3hfz.js",
		"assets/monaco/workers-Bip08C9l.js",
	}
	paths := siteFileSet(site)
	for _, path := range want {
		if !paths[path] {
			t.Errorf("missing %s in bundle", path)
		}
	}
	index := mustRead(t, filepath.Join(root, "index.html"))
	if !IsSlidevIndex(index) {
		t.Fatal("fixture index should be detected as Slidev")
	}
	for _, warning := range site.Warnings {
		if strings.Contains(warning, "different devices") || strings.Contains(warning, "frame-src") {
			t.Fatalf("unexpected static-hosting warning: %s", warning)
		}
	}
}

func TestIsSlidevIndex(t *testing.T) {
	fixture := mustRead(t, filepath.Join("testdata", "slidev-dist", "index.html"))
	if !IsSlidevIndex(fixture) {
		t.Fatal("fixture should match")
	}
	if IsSlidevIndex([]byte("<html><head><title>Slidev</title></head></html>")) {
		t.Fatal("title-only fallback must not match")
	}
	if !IsSlidevIndex([]byte(`<html><head><script src="/assets/slidev/foo.js"></script></head></html>`)) {
		t.Fatal("slidev asset path should match")
	}
}

func TestValidateSitePathAllowsSlidevArtifacts(t *testing.T) {
	for _, path := range []string{"404.html", "_redirects", "assets/editor.worker-BbVB3hfz.js", "slidev-exported.pdf"} {
		if message := ValidateSitePath(path); message != "" {
			t.Errorf("%s: %s", path, message)
		}
	}
}

func TestSlidevMissingReferencedAsset(t *testing.T) {
	index := mustRead(t, filepath.Join("testdata", "slidev-dist", "index.html"))
	warnings := SlidevWarnings(SiteResult{Files: []SiteFile{{Path: "index.html"}}}, index)
	if !warningContains(warnings, "missing from the build") {
		t.Fatalf("expected missing asset warnings, got %v", warnings)
	}
}

func TestSlidevWarningsAbsentWhenNotSlidev(t *testing.T) {
	warnings := SlidevWarnings(SiteResult{}, []byte("<html><body>hello</body></html>"))
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevBundleNearLimitWarning(t *testing.T) {
	index := mustRead(t, filepath.Join("testdata", "slidev-dist", "index.html"))
	site := SiteResult{
		Bytes: 46 * 1024 * 1024,
		Files: siteFilesFromSet(siteFileSetFromFixture()),
	}
	warnings := SlidevWarnings(site, index)
	if !warningContains(warnings, WarningSlidevBundleNearLimit) {
		t.Fatalf("warnings = %v", warnings)
	}
	site.Bytes = 1024
	warnings = SlidevWarnings(site, index)
	if warningContains(warnings, WarningSlidevBundleNearLimit) {
		t.Fatalf("unexpected near-limit warning: %v", warnings)
	}
}

func TestSlidevSubpathBaseWarning(t *testing.T) {
	index := []byte(`<html><head><meta property="slidev:version" content="1"><link href="/custom-base/app.js"></head></html>`)
	site := SiteResult{Files: []SiteFile{{Path: "index.html"}}}
	warnings := SlidevWarnings(site, index)
	if !warningContains(warnings, "subpath base") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevMonacoWorkersWarning(t *testing.T) {
	index := []byte(`<html><head><meta property="slidev:version" content="1"><script>monaco</script></head></html>`)
	warnings := SlidevWarnings(SiteResult{Files: []SiteFile{{Path: "index.html"}}}, index)
	if !warningContains(warnings, WarningSlidevMonacoWorkers) {
		t.Fatalf("warnings = %v", warnings)
	}
	warnings = SlidevWarnings(SiteResult{Files: []SiteFile{
		{Path: "index.html"},
		{Path: "assets/editor.worker-x.js"},
	}}, index)
	if warningContains(warnings, WarningSlidevMonacoWorkers) {
		t.Fatalf("unexpected monaco warning: %v", warnings)
	}
}

func TestSlidevSourceWarningsRequirePDF(t *testing.T) {
	dir := t.TempDir()
	writeSlides(t, dir, "---\ndownload: true\n---\n")
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 1 || !strings.Contains(warnings[0], "slidev-exported.pdf") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevSourceWarningsDownloadQuotedTrue(t *testing.T) {
	dir := t.TempDir()
	writeSlides(t, dir, "---\ndownload: \"true\"\n---\n")
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevSourceWarningsNoDownloadNoPDFWarning(t *testing.T) {
	dir := t.TempDir()
	writeSlides(t, dir, "---\ntitle: deck\n---\n")
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevSourceWarningsCustomExportFilename(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "dist"), 0o755)
	writeSlides(t, dir, "---\ndownload: true\nexportFilename: my-deck\n---\n")
	warnings := SlidevSourceWarnings(dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "my-deck.pdf") {
		t.Fatalf("warnings = %v", warnings)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "my-deck.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevSourceWarningsDrawingsPersist(t *testing.T) {
	dir := t.TempDir()
	writeSlides(t, dir, "---\ndrawings:\n  persist: true\n---\n")
	warnings := SlidevSourceWarnings(dir)
	if len(warnings) != 1 || warnings[0] != WarningSlidevDrawingsPersist {
		t.Fatalf("warnings = %v", warnings)
	}
	writeSlides(t, dir, "---\ndrawings:\n  persist: false\n---\n")
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSlidevSourceWarningsRemotePassword(t *testing.T) {
	dir := t.TempDir()
	writeSlides(t, dir, "---\nremote: secret\n---\n")
	warnings := SlidevSourceWarnings(dir)
	if len(warnings) != 1 || warnings[0] != WarningSlidevRemotePassword {
		t.Fatalf("warnings = %v", warnings)
	}
	writeSlides(t, dir, "---\nremote:\n  password: talk\n---\n")
	warnings = SlidevSourceWarnings(dir)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
	writeSlides(t, dir, "---\nremote: false\n---\n")
	if warnings := SlidevSourceWarnings(dir); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func siteFileSetFromFixture() map[string]bool {
	root := filepath.Join("testdata", "slidev-dist")
	site, err := CollectSite(root)
	if err != nil {
		panic(err)
	}
	return siteFileSet(site)
}

func siteFilesFromSet(set map[string]bool) []SiteFile {
	files := make([]SiteFile, 0, len(set))
	for path := range set {
		files = append(files, SiteFile{Path: path})
	}
	return files
}

func writeSlides(t *testing.T, dir string, content string) {
	if err := os.WriteFile(filepath.Join(dir, "slides.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func warningContains(warnings []string, substr string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substr) {
			return true
		}
	}
	return false
}

func mustRead(t *testing.T, path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
