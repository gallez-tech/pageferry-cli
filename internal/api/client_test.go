package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeSendsAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer pf_test" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("User-Agent"); got != "pageferry/1.2.3" {
			t.Errorf("user agent = %q", got)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"accountId":"a","accountName":"Account","apiKeyId":"k","apiKeyName":"Key"}`))
	}))
	defer server.Close()
	identity, err := (&Client{Origin: server.URL, APIKey: "pf_test", Version: "1.2.3"}).Me(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.AccountName != "Account" || identity.APIKeyName != "Key" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestAPIErrorIncludesAllMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = response.Write([]byte(`{"error":"Invalid upload.","errors":["bad html","bad name"]}`))
	}))
	defer server.Close()
	_, err := (&Client{Origin: server.URL, APIKey: "x"}).Me(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid upload.") || !strings.Contains(err.Error(), "bad html") || !strings.Contains(err.Error(), "bad name") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateOrigin(t *testing.T) {
	if got, err := ValidateOrigin(" https://example.com/// "); err != nil || got != "https://example.com" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, invalid := range []string{"example.com", "ftp://example.com", "https://example.com/api", "https://u@example.com"} {
		if _, err := ValidateOrigin(invalid); err == nil {
			t.Errorf("ValidateOrigin(%q) succeeded", invalid)
		}
	}
}
