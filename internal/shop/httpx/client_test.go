package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"parfumes_finder/internal/shop"
)

func TestGetDocumentSendsStableHeadersAndParsesHTML(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != "PerfumePriceBot/0.1" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := request.Header.Get("Accept-Language"); got != "ru-RU,ru;q=0.9" {
			t.Errorf("Accept-Language = %q", got)
		}
		_, _ = io.WriteString(writer, `<html><body><h1>Найдено</h1></body></html>`)
	}))
	defer server.Close()

	client := New(server.Client())
	document, err := client.GetDocument(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(document.Find("h1").Text()); got != "Найдено" {
		t.Fatalf("h1 = %q", got)
	}
}

func TestGetDocumentClassifiesForbidden(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := New(server.Client()).GetDocument(context.Background(), server.URL)
	if got := shop.ErrorKindOf(err); got != shop.ErrorAccess {
		t.Fatalf("error kind = %q, want %q", got, shop.ErrorAccess)
	}
}

func TestGetDocumentRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Repeat("x", 5*1024*1024+1))
	}))
	defer server.Close()

	_, err := New(server.Client()).GetDocument(context.Background(), server.URL)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
}

func TestPostFormReturnsBoundedBytes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Form.Get("cmd") != "live_search" || request.Form.Get("text") != "dior sauvage" {
			t.Errorf("form = %v", request.Form)
		}
		_, _ = io.WriteString(writer, `"<div>result</div>"`)
	}))
	defer server.Close()

	body, err := New(server.Client()).PostForm(context.Background(), server.URL, url.Values{
		"cmd":  {"live_search"},
		"text": {"dior sauvage"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `"<div>result</div>"` {
		t.Fatalf("body = %q", body)
	}
}

func TestValidateURLHostRejectsStoreSuppliedExternalLinks(t *testing.T) {
	if err := ValidateURLHost("https://randewoo.ru/product/x", "https://randewoo.ru"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://evil.example/product", "javascript:alert(1)", "https://randewoo.ru.evil.example/x"} {
		if err := ValidateURLHost(raw, "https://randewoo.ru"); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
