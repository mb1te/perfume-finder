package orental

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

func TestAdapterSearchExpandsOrentalProduct(t *testing.T) {
	productHTML, err := os.ReadFile("testdata/product-sauvage.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/":
			_, _ = io.WriteString(w, `<div class="prod-teaser"><a class="prod-teaser__title" href="/sauvage/"><strong class="prod-teaser__brand">Christian Dior</strong><span class="prod-teaser__name">Sauvage</span></a></div>`)
		case "/sauvage/":
			_, _ = w.Write(productHTML)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := newAdapter(httpx.New(server.Client()), time.Now, server.URL)
	offers, err := adapter.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) == 0 {
		t.Fatal("Search returned no offers")
	}
}
