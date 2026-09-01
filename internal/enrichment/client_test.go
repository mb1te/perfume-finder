package enrichment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"parfumes_finder/internal/domain"
)

var request = Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}

var wantedCard = Card{
	SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
	Title:     "Sauvage Eau de Parfum Dior",
	Accords:   []Accord{{Name: "fresh spicy", Color: "#d8c69a", Width: 100}},
	PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
}

func TestHTTPClientSendsBoundedJSONRequestAndDecodesCard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, got *http.Request) {
		if got.Method != http.MethodPost || got.URL.Path != "/v1/enrich" {
			t.Fatalf("request = %s %s", got.Method, got.URL.Path)
		}
		if contentType := got.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("content type = %q", contentType)
		}
		var input Request
		if err := json.NewDecoder(http.MaxBytesReader(writer, got.Body, 16<<10)).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !reflect.DeepEqual(input, request) {
			t.Fatalf("request = %+v, want %+v", input, request)
		}
		writeJSONCard(t, writer, wantedCard)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := client.Enrich(context.Background(), request)
	if err != nil || !ok || !reflect.DeepEqual(got, wantedCard) {
		t.Fatalf("got %+v, ok=%v, err=%v", got, ok, err)
	}
}

func TestHTTPClientMapsNotFoundAndOtherStatuses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		ok     bool
	}{
		{name: "not found", status: http.StatusNotFound},
		{name: "bad gateway", status: http.StatusBadGateway},
		{name: "busy", status: http.StatusTooManyRequests},
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "timeout", status: http.StatusGatewayTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
			}))
			defer server.Close()
			client, err := NewHTTPClient(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, ok, err := client.Enrich(context.Background(), request)
			if test.status == http.StatusNotFound {
				if err != nil || ok {
					t.Fatalf("ok=%v, err=%v", ok, err)
				}
				return
			}
			var statusErr *HTTPStatusError
			if ok || !errors.As(err, &statusErr) || statusErr.StatusCode != test.status {
				t.Fatalf("ok=%v, err=%v", ok, err)
			}
		})
	}
}

func TestHTTPClientReturnsRedirectStatusWithoutFollowingIt(t *testing.T) {
	var redirectCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, got *http.Request) {
		switch got.URL.Path {
		case "/v1/enrich":
			writer.Header().Set("Location", "/redirect-target")
			writer.WriteHeader(http.StatusFound)
		case "/redirect-target":
			redirectCalled.Store(true)
			writeJSONCard(t, writer, wantedCard)
		default:
			http.NotFound(writer, got)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := client.Enrich(context.Background(), request)
	var statusErr *HTTPStatusError
	if ok || !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusFound {
		t.Fatalf("ok=%v, err=%v", ok, err)
	}
	if redirectCalled.Load() {
		t.Fatal("redirect target was called")
	}
}

func TestHTTPClientRejectsOversizedRequestBeforeCallingRemote(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	oversized := request
	oversized.Name = strings.Repeat("x", 16<<10)
	if _, _, err := client.Enrich(context.Background(), oversized); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("remote calls = %d, want 0", calls)
	}
}

func TestHTTPClientRejectsResponseOverSevenMiB(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"source_url":"https://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html","title":"`))
		_, _ = writer.Write([]byte(strings.Repeat("x", 7<<20)))
		_, _ = writer.Write([]byte(`","png_base64":"iVBORw0KGgo="}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPClientRejectsInvalidContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writeJSONCard(t, writer, wantedCard)
	}))
	defer server.Close()
	client, _ := NewHTTPClient(server.URL, server.Client())
	if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrInvalidContentType) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPClientRejectsInvalidBase64AndPNG(t *testing.T) {
	for _, test := range []struct {
		name      string
		pngBase64 string
	}{
		{name: "base64", pngBase64: "%"},
		{name: "signature", pngBase64: base64.StdEncoding.EncodeToString([]byte("not-png"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"source_url": wantedCard.SourceURL,
					"title":      wantedCard.Title,
					"png_base64": test.pngBase64,
				})
			}))
			defer server.Close()
			client, _ := NewHTTPClient(server.URL, server.Client())
			if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrInvalidImage) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHTTPClientRejectsDecodedPNGOverFiveMiB(t *testing.T) {
	png := append(append([]byte(nil), wantedCard.PNG...), bytes.Repeat([]byte{0}, 5<<20)...)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"source_url": wantedCard.SourceURL,
			"title":      wantedCard.Title,
			"png_base64": base64.StdEncoding.EncodeToString(png),
		})
	}))
	defer server.Close()
	client, _ := NewHTTPClient(server.URL, server.Client())
	if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPClientRejectsUnsafeFragranticaSourceURL(t *testing.T) {
	for _, sourceURL := range []string{
		"https://evil.example/perfume/Dior/Sauvage-31861.html",
		"http://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html",
		"https://user@www.fragrantica.ru/perfume/Dior/Sauvage-31861.html",
		"https://www.fragrantica.ru:444/perfume/Dior/Sauvage-31861.html",
		"https://www.fragrantica.ru./perfume/Dior/Sauvage-31861.html",
		"https://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html?tracking=1",
		"https://www.fragrantica.ru/perfume/Dior/../Sauvage-31861.html",
		"https://www.fragrantica.ru/perfume/Dior/%2e%2e/Sauvage-31861.html",
		"https://www.fragrantica.ru/perfume//Dior/Sauvage-31861.html",
		"https://www.fragrantica.ru/search/?query=Sauvage",
	} {
		t.Run(sourceURL, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				card := wantedCard
				card.SourceURL = sourceURL
				writeJSONCard(t, writer, card)
			}))
			defer server.Close()
			client, _ := NewHTTPClient(server.URL, server.Client())
			if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrInvalidSourceURL) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNewHTTPClientRejectsInvalidBaseURL(t *testing.T) {
	for _, baseURL := range []string{"", "//sidecar", "ftp://sidecar", "https://user@sidecar", "https://sidecar/?query=1"} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewHTTPClient(baseURL, nil); err == nil {
				t.Fatal("NewHTTPClient error = nil")
			}
		})
	}
}

func writeJSONCard(t *testing.T, writer http.ResponseWriter, card Card) {
	t.Helper()
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	if err := json.NewEncoder(writer).Encode(map[string]any{
		"source_url": card.SourceURL,
		"title":      card.Title,
		"accords":    card.Accords,
		"png_base64": base64.StdEncoding.EncodeToString(card.PNG),
	}); err != nil {
		t.Fatal(err)
	}
}
