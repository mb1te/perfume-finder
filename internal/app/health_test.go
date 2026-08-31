package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandlerIsUnavailableUntilReady(t *testing.T) {
	readiness := NewReadiness()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	before := httptest.NewRecorder()
	readiness.Handler().ServeHTTP(before, request)
	if before.Code != http.StatusServiceUnavailable {
		t.Fatalf("before=%d", before.Code)
	}
	readiness.MarkReady()
	after := httptest.NewRecorder()
	readiness.Handler().ServeHTTP(after, request)
	if after.Code != http.StatusOK {
		t.Fatalf("after=%d", after.Code)
	}
}
