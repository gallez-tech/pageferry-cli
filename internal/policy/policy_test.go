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

func TestValidateHTMLAcceptedSignals(t *testing.T) {
	result := ValidateHTML([]byte(`<!doctype html><title> Plan </title><style>body { color: red }</style><script>noop()</script><img src="https://B.example/a"><img src="//a.example/a">`))
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

func TestValidateHTMLRejectsDangerousContentAndDeduplicates(t *testing.T) {
	result := ValidateHTML([]byte(`<form><a href="java script:alert(1)" onclick="x()">x</a><div style="behavior : url(x)"></div><style>x{background:url( javascript:alert(1))}</style><script src="x.js"></script><script src="y.js"></script><meta http-equiv="refresh"></form>`))
	want := []string{
		"Blocked <form> tag found.",
		`Blocked inline event handler attribute "onclick" found.`,
		`Blocked unsafe URL in "href" attribute.`,
		"Blocked unsafe inline CSS.",
		"External script sources are not allowed.",
		"Blocked meta refresh tag found.",
	}
	for _, expected := range want {
		if !contains(result.Errors, expected) {
			t.Errorf("missing %q in %v", expected, result.Errors)
		}
	}
	count := 0
	for _, value := range result.Errors {
		if value == "External script sources are not allowed." {
			count++
		}
	}
	if count != 1 {
		t.Errorf("external script error appeared %d times", count)
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
