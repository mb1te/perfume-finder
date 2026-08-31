package allure

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/shop/httpx"
)

func TestAdapterSearchExpandsProductVariants(t *testing.T) {
	t.Parallel()

	productHTML, err := os.ReadFile("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/search/":
			if got := request.URL.Query().Get("q"); got != "Dior Sauvage" {
				t.Errorf("query = %q", got)
			}
			_, _ = io.WriteString(writer, `<div class="catalog-grid__item"><a class="product-card__link" href="/product/sauvage"><div class="product-card__brand">Christian Dior</div><h5 class="product-card__name">Sauvage 2015</h5></a></div>`)
		case "/product/sauvage":
			_, _ = writer.Write(productHTML)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	clock := func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }
	adapter := newAdapter(httpx.New(server.Client()), clock, server.URL)
	offers, err := adapter.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
	if err != nil {
		t.Fatal(err)
	}
	if len(offers) == 0 {
		t.Fatal("Search() returned no offers")
	}
	for _, offer := range offers {
		if !strings.HasPrefix(offer.URL, server.URL+"/product/sauvage?el=") {
			t.Fatalf("unsafe or unexpected offer URL %q", offer.URL)
		}
	}
}

func TestAdapterHealthRequiresParseableSearch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `<html><body>markup changed</body></html>`)
	}))
	defer server.Close()

	adapter := newAdapter(httpx.New(server.Client()), time.Now, server.URL)
	result := adapter.Health(context.Background())
	if result.Status != shop.HealthDegraded || result.Err == nil {
		t.Fatalf("Health() = %+v, want degraded error", result)
	}
}
