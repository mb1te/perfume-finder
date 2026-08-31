package duhirf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop/httpx"
	"testing"
	"time"
)

func TestAdapterUsesLiveSearchPOST(t *testing.T) {
	product, err := os.ReadFile("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.php" {
			_ = r.ParseForm()
			if r.Form.Get("cmd") != "live_search" {
				t.Error(r.Form)
			}
			html := `<a class="term_res_prod" data-product="16031" data-model="Christian Dior - Sauvage 2015" href="/product"></a>`
			_ = json.NewEncoder(w).Encode(html)
			return
		}
		if r.URL.Path == "/product" {
			_, _ = w.Write(product)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	adapter := newAdapter(httpx.New(server.Client()), time.Now, server.URL)
	offers, err := adapter.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if err != nil || len(offers) == 0 {
		t.Fatalf("offers=%d err=%v", len(offers), err)
	}
}
