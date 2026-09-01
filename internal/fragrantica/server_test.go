package fragrantica

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

type fakeRenderer struct {
	card       enrichment.Card
	ok         bool
	err        error
	readyCalls atomic.Int32
}

func (renderer *fakeRenderer) Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error) {
	return renderer.card, renderer.ok, renderer.err
}

func (renderer *fakeRenderer) Ready(context.Context) error {
	renderer.readyCalls.Add(1)
	return renderer.err
}

func TestServerReturnsBoundedJSONCard(t *testing.T) {
	renderer := &fakeRenderer{card: enrichment.Card{
		SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Title:     "Sauvage Eau de Parfum Dior",
		PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}, ok: true}
	request := httptest.NewRequest(http.MethodPost, "/v1/enrich", strings.NewReader(`{
		"brand":"Dior","name":"Sauvage","concentration":"edp"
	}`))
	response := httptest.NewRecorder()
	NewServer(renderer).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	var body struct {
		PNGBase64 string `json:"png_base64"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(body.PNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, renderer.card.PNG) {
		t.Fatal("PNG mismatch")
	}
}

func TestServerMapsEnrichmentOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		card   enrichment.Card
		ok     bool
		err    error
		status int
	}{
		{name: "no exact match", status: http.StatusNotFound},
		{name: "busy", err: ErrBusy, status: http.StatusTooManyRequests},
		{name: "access challenge", err: ErrAccessChallenge, status: http.StatusServiceUnavailable},
		{name: "deadline", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout},
		{name: "renderer failure", err: errors.New("chrome crashed"), status: http.StatusBadGateway},
		{name: "image over five MiB", card: enrichment.Card{PNG: make([]byte, 5*1024*1024+1)}, ok: true, status: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			renderer := &fakeRenderer{card: test.card, ok: test.ok, err: test.err}
			response := httptest.NewRecorder()
			NewServer(renderer).ServeHTTP(response, validEnrichRequest())
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestServerRejectsRequestBodyOverSixteenKiB(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/enrich", strings.NewReader(`{"brand":"Dior","name":"`+strings.Repeat("x", 16*1024)+`","concentration":"edp"}`))
	response := httptest.NewRecorder()
	NewServer(&fakeRenderer{}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestServerBoundsMetadataAndKeepsFiveMiBResponseUnderSevenMiB(t *testing.T) {
	longUTF8 := strings.Repeat("я", 2_100)
	accords := make([]enrichment.Accord, 20)
	for index := range accords {
		accords[index] = enrichment.Accord{Name: longUTF8, Color: longUTF8, Width: 100}
	}
	renderer := &fakeRenderer{card: enrichment.Card{
		SourceURL: longUTF8,
		Title:     longUTF8,
		Accords:   accords,
		PNG:       make([]byte, 5*1024*1024),
	}, ok: true}
	response := httptest.NewRecorder()
	NewServer(renderer).ServeHTTP(response, validEnrichRequest())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response.Body.Len() > 7*1024*1024 {
		t.Fatalf("response = %d bytes, exceeds 7 MiB", response.Body.Len())
	}
	t.Logf("response-bound evidence: 5 MiB PNG encoded into %d-byte JSON response (limit %d)", response.Body.Len(), 7*1024*1024)
	var body enrichResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Title) > 256 || !utf8.ValidString(body.Title) {
		t.Fatalf("title is not UTF-8-safe at 256 bytes: %d %#q", len(body.Title), body.Title)
	}
	if len(body.SourceURL) > 2048 || !utf8.ValidString(body.SourceURL) {
		t.Fatalf("source URL is not UTF-8-safe at 2048 bytes: %d %#q", len(body.SourceURL), body.SourceURL)
	}
	if len(body.Accords) != 16 {
		t.Fatalf("accord count = %d, want 16", len(body.Accords))
	}
	for index, accord := range body.Accords {
		if len(accord.Name) > 128 || !utf8.ValidString(accord.Name) || len(accord.Color) > 128 || !utf8.ValidString(accord.Color) {
			t.Fatalf("accord %d is not bounded UTF-8: %+v", index, accord)
		}
	}
}

func TestServerHealthChecksBrowserReadiness(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "ready", status: http.StatusOK},
		{name: "not ready", err: errors.New("chrome unavailable"), status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			renderer := &fakeRenderer{err: test.err}
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			response := httptest.NewRecorder()
			NewServer(renderer).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if renderer.readyCalls.Load() != 1 {
				t.Fatalf("Ready calls = %d, want 1", renderer.readyCalls.Load())
			}
		})
	}
}

func validEnrichRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/v1/enrich", strings.NewReader(`{"brand":"Dior","name":"Sauvage","concentration":"edp"}`))
}

var browserRequest = enrichment.Request{
	Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
}
