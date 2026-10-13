package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{" report.html ", true},
		{"report.HTML", false},
		{"../report.html", false},
		{"raw.html", false},
		{"report.html?download", false},
		{strings.Repeat("é", 63) + ".html", false},
	}
	for _, test := range tests {
		normalized, errors := ValidateFilename(test.name)
		if got := len(errors) == 0; got != test.ok {
			t.Errorf("ValidateFilename(%q) ok = %v, errors = %v", test.name, got, errors)
		}
		if test.ok && normalized != "report.html" {
			t.Errorf("normalized = %q", normalized)
		}
	}
}

func TestValidateHTMLAcceptsRealisticAlpineAndHTMX(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html><head><title> Plan </title><link rel="stylesheet" href="https://cdn.example/app.css"><style>@font-face { font-family: Report; src: url("https://cdn.example/report.woff2") }</style></head><body><section x-data="{ open: false }"><button type="button" x-on:click="open = !open">Toggle</button><p x-show="open" x-transition>Alpine content</p></section><form hx-post="https://api.example/action" hx-target="#result" hx-swap="outerHTML"><input name="query"><button>Send</button></form><output id="result"></output><script src="https://cdn.example/alpine.js" defer></script><script src="https://cdn.example/htmx.js"></script><script type="module" src="//cdn.example/app.js"></script><script type="text/javascript">window.reportReady = true</script><script type="application/javascript">window.reportLoaded = true</script><img src="https://B.example/a"><img src="//a.example/a"></body></html>`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if result.Title != "Plan" || !result.HasInlineScript {
		t.Fatalf("unexpected signals: %#v", result)
	}
	want := "a.example,b.example"
	if got := strings.Join(result.ExternalImageHosts, ","); got != want {
		t.Fatalf("hosts = %q, want %q", got, want)
	}
}

func TestValidateHTMLRemovedRulesProduceNothing(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html lang="en"><head><title>x</title><meta name="viewport" content="width=device-width, initial-scale=1"><style>main { scroll-behavior: smooth; background: url(javascript:x) }</style></head><body><iframe srcdoc="a" src="https://example.com"></iframe><a href="javascript:void(0)" onclick="x()">x</a><script type="application/ld+json">{}</script></body></html>`))
	if len(result.Errors) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("errors = %v, warnings = %v", result.Errors, result.Warnings)
	}
}

func TestValidateHTMLWarnsAboutContentThatWillNotWorkAndDeduplicates(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html lang="en"><head><title>x</title><meta name="viewport" content="width=device-width, initial-scale=1"><base href="https://example.com"><meta http-equiv="refresh" content="5"></head><body><object></object><embed><applet></applet><a href="file:///a">a</a><a href="FILE:///b">b</a><script src="x.js"></script><script src="http://example.com/y.js"></script><script src="data:text/javascript,1"></script></body></html>`))
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %v", result.Errors)
	}
	assertWarnings(t, result, WarningBase, WarningMetaRefresh, WarningPlugins, WarningFileURL, WarningRelativeScript, WarningInsecureScript)
}

func TestValidateHTMLPreservesSizeLimit(t *testing.T) {
	result := ValidateHTML([]byte(strings.Repeat("x", MaxHTMLBytes+1)))
	expected := "HTML document is 524289 bytes; maximum is 524288 bytes."
	if !contains(result.Errors, expected) {
		t.Fatalf("missing %q in %v", expected, result.Errors)
	}
}

func TestValidateHTMLPreservesDepthLimit(t *testing.T) {
	nested := strings.Repeat("<div>", 520) + "content" + strings.Repeat("</div>", 520)
	result := ValidateHTML([]byte(nested))
	expected := "HTML is nested more than 512 levels deep."
	if !contains(result.Errors, expected) {
		t.Fatalf("missing %q in %v", expected, result.Errors)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestRelativeScriptsOnlyAllowedForSites(t *testing.T) {
	document := []byte(`<!doctype html><html lang="en"><title>x</title><meta name="viewport" content="width=device-width, initial-scale=1"><script type="module" src="/assets/index-abc.js"></script>`)
	assertWarnings(t, ValidateHTML(document), WarningRelativeScript)
	assertWarnings(t, ValidateHTMLWithOptions(document, HTMLOptions{AllowRelativeScripts: true}))
	insecure := []byte(`<!doctype html><html lang="en"><title>x</title><meta name="viewport" content="width=device-width, initial-scale=1"><script src="http://example.com/x.js"></script>`)
	assertWarnings(t, ValidateHTMLWithOptions(insecure, HTMLOptions{AllowRelativeScripts: true}), WarningInsecureScript)
}

func TestValidateHTMLFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "policy", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(strings.TrimSuffix(path, ".json") + ".html")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Mode     string   `json:"mode"`
				Errors   []string `json:"errors"`
				Warnings []string `json:"warnings"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			options := HTMLOptions{}
			switch fixture.Mode {
			case "document":
			case "site":
				options.AllowRelativeScripts = true
			default:
				t.Fatalf("unknown mode %q", fixture.Mode)
			}
			result := ValidateHTMLWithOptions(content, options)
			assertSameSet(t, "errors", result.Errors, fixture.Errors)
			assertSameSet(t, "warnings", result.Warnings, fixture.Warnings)
		})
	}
}

func assertSameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", label, got, want)
	}
}

func TestValidateSitePath(t *testing.T) {
	for _, valid := range []string{"index.html", "assets/index-abc.js", "404.html"} {
		if message := ValidateSitePath(valid); message != "" {
			t.Errorf("%s: %s", valid, message)
		}
	}
	for _, invalid := range []string{"", "/index.html", "../x", "a//b", ".env", "a/.git/config", "__pageferry/x", `a\b`, "a?b"} {
		if ValidateSitePath(invalid) == "" {
			t.Errorf("%q was accepted", invalid)
		}
	}
}

func TestValidateHTMLReportsNoMobileWarningsForPhoneReadyDocument(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html lang="en"><head>
      <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
      <title>Report</title>
      <style>
        main { max-width: 960px; min-height: 100vh; min-height: 100svh; }
        @media (min-width: 700px) { main { padding: 2rem; } }
        input, select { font-size: 16px; }
        .input-hint { font-size: 12px; }
      </style>
    </head><body><main><input style="font: 1rem system-ui"></main></body></html>`))
	if len(result.Errors) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("errors = %v, warnings = %v", result.Errors, result.Warnings)
	}
}

func TestValidateHTMLWarnsAboutMobileHostileMarkup(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html><head><title>Report</title>
      <style>
        body { width: 960px; }
        .hero { height: 100vh; }
        /* select { font-size: 16px; } */
        select { font: 600 14px/1.2 sans-serif; }
      </style>
    </head><body><p>Hi</p></body></html>`))
	assertWarnings(t, result, WarningNoViewport, WarningFixedWidth, WarningSmallFormControlFont, WarningViewportHeight, WarningNoLang)
}

func TestValidateHTMLWarnsAboutRestrictiveViewports(t *testing.T) {
	noZoom := ValidateHTML([]byte(`<!doctype html><html lang="en"><title>x</title>
      <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">`))
	assertWarnings(t, noZoom, WarningZoomDisabled)

	unscalable := ValidateHTML([]byte(`<!doctype html><html lang="en"><title>x</title>
      <meta name="viewport" content="width=device-width,user-scalable=no">`))
	assertWarnings(t, unscalable, WarningZoomDisabled, WarningNoInitialScale)
}

func TestValidateHTMLWarnsAboutSmallInlineFormControlFonts(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><html lang="en"><title>x</title>
      <meta name="viewport" content="width=device-width, initial-scale=1">
      <textarea style="font-size: .8rem"></textarea><p style="font-size: 12px">note</p>`))
	assertWarnings(t, result, WarningSmallFormControlFont)
}

func assertWarnings(t *testing.T, result HTMLResult, want ...string) {
	t.Helper()
	if !slices.Equal(result.Warnings, want) {
		t.Fatalf("warnings = %q, want %q", result.Warnings, want)
	}
}
