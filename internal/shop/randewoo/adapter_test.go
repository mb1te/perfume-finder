package randewoo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop/httpx"
	"strings"
	"testing"
	"time"
)

func TestAdapterVerifiesAutocompleteCandidateOnProductPage(t *testing.T) {
	product, err := os.ReadFile("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/autocomplete" {
			_ = json.NewEncoder(w).Encode(map[string]any{"products": []any{map[string]any{"id": "437349", "available": true, "name": "Sauvage 2015", "brand": "Christian Dior", "link_url": serverURL(r) + "/product?preferred=437349", "categories": []any{map[string]any{"name": "Пробники (парфюмерия - мужская)"}}}}})
			return
		}
		if r.URL.Path == "/product" {
			_, _ = w.Write([]byte(strings.ReplaceAll(string(product), "https://randewoo.ru", serverURL(r))))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	adapter := newAdapter(httpx.New(server.Client()), time.Now, server.URL+"/autocomplete", server.URL)
	offers, err := adapter.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if err != nil || len(offers) == 0 {
		t.Fatalf("offers=%d err=%v", len(offers), err)
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }
