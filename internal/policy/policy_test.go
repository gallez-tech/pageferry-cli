package policy

import (
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

func TestValidateHTMLAcceptsEverySupportedScriptType(t *testing.T) {
	for _, typ := range []string{"", "module", "text/javascript", "application/javascript"} {
		attribute := ""
		if typ != "" {
			attribute = ` type="` + typ + `"`
		}
		result := ValidateHTML([]byte(`<title>Scripts</title><script` + attribute + ` src="https://cdn.example/app.js"></script>`))
		if len(result.Errors) != 0 {
			t.Errorf("type %q: unexpected errors: %v", typ, result.Errors)
		}
	}
}

func TestValidateHTMLRejectsDangerousContentAndDeduplicates(t *testing.T) {
	result := ValidateHTML([]byte(`<iframe srcdoc="unsafe"></iframe><object></object><embed><applet></applet><base href="https://example.com"><a href="java script:alert(1)" onclick="x()">x</a><div style="behavior : url(x)"></div><style>x{background:url( javascript:alert(1))}</style><script src="x.js"></script><script src="http://example.com/y.js"></script><script type="application/ld+json">{}</script><meta http-equiv="refresh">`))
	want := []string{
		"Blocked <iframe> tag found.",
		"Blocked <object> tag found.",
		"Blocked <embed> tag found.",
		"Blocked <applet> tag found.",
		"Blocked <base> tag found.",
		`Blocked "srcdoc" attribute found.`,
		`Blocked inline event handler attribute "onclick" found.`,
		`Blocked unsafe URL in "href" attribute.`,
		"Blocked unsafe inline CSS.",
		"External script sources must use HTTPS.",
		`Unsupported script type "application/ld+json" found.`,
		"Blocked meta refresh tag found.",
	}
	for _, expected := range want {
		if !contains(result.Errors, expected) {
			t.Errorf("missing %q in %v", expected, result.Errors)
		}
	}
	count := 0
	for _, value := range result.Errors {
		if value == "External script sources must use HTTPS." {
			count++
		}
	}
	if count != 1 {
		t.Errorf("external script error appeared %d times", count)
	}
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
