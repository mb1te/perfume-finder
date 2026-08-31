package aromabutik

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop/httpx"
)

func TestAdapterSearchExpandsProduct(t *testing.T) {
	product, err := os.ReadFile("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/advanced_search_result.php":
			_, _ = io.WriteString(w, `<div class="ex_product_item"><div class="ex_product_name"><a href="/sauvage/"><span class="brand">Christian Dior</span>Sauvage 2015</a></div></div>`)
		case "/sauvage/":
			_, _ = w.Write(product)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := newAdapter(httpx.New(server.Client()), time.Now, server.URL)
	offers, err := adapter.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if err != nil || len(offers) == 0 {
		t.Fatalf("offers=%d err=%v", len(offers), err)
	}
}
