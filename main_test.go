package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvidersV3Schema(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v3/providers", nil)
	rec := httptest.NewRecorder()

	mux := newTestMux()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"pricing"`) {
		t.Errorf("v3 response should contain nested pricing object")
	}
	if strings.Contains(rec.Body.String(), `"cost_per_1m_in"`) {
		t.Errorf("v3 response should not contain legacy cost_per_1m_in field")
	}
}

func TestProvidersV2Schema(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v2/providers", nil)
	rec := httptest.NewRecorder()

	mux := newTestMux()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"cost_per_1m_in"`) {
		t.Errorf("v2 response should contain legacy cost_per_1m_in field")
	}
	if !strings.Contains(body, `"supports_attachments"`) {
		t.Errorf("v2 response should contain legacy supports_attachments field")
	}
	if strings.Contains(body, `"pricing"`) {
		t.Errorf("v2 response should not contain nested pricing object")
	}
	if strings.Contains(body, `"session_affinity_header"`) {
		t.Errorf("v2 response should not contain session_affinity_header")
	}

	var providers []map[string]any
	if err := json.Unmarshal([]byte(body), &providers); err != nil {
		t.Fatalf("v2 response is not valid JSON: %v", err)
	}
}

func TestProvidersV2ETag(t *testing.T) {
	mux := newTestMux()

	req := httptest.NewRequest(http.MethodGet, "/v2/providers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	tag := rec.Header().Get("ETag")
	if tag == "" {
		t.Fatal("expected ETag header")
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/providers", nil)
	req.Header.Set("If-None-Match", tag)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304 for matching ETag, got %d", rec.Code)
	}
}

func newTestMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/providers", providersHandler(providersJSON, providersETag))
	mux.HandleFunc("/v2/providers", providersHandler(providersV2JSON, providersV2ETag))
	mux.HandleFunc("/providers", providersHandlerDeprecated)
	return mux
}
