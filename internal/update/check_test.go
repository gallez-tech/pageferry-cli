package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type rewriteTransport struct {
	target string
}

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	parsed, _ := http.NewRequest(http.MethodGet, transport.target, nil)
	clone.URL = parsed.URL
	return http.DefaultTransport.RoundTrip(clone)
}

func TestCheckReportsOutdatedRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"tag_name":"v1.4.0","html_url":"https://example.test/release"}`))
	}))
	defer server.Close()

	result, err := Check(context.Background(), "1.3.9", &http.Client{
		Transport: rewriteTransport{target: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Outdated || result.Latest != "1.4.0" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDevelopmentVersionDoesNotClaimToBeOutdated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write([]byte(`{"tag_name":"v1.4.0"}`))
	}))
	defer server.Close()

	result, err := Check(context.Background(), "dev", &http.Client{
		Transport: rewriteTransport{target: server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outdated {
		t.Fatalf("result = %#v", result)
	}
}
