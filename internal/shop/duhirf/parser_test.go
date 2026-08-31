package duhirf

import (
	"os"
	"parfumes_finder/internal/domain"
	"testing"
	"time"
)

func TestParseLiveSearchDecodesJSONWrappedHTML(t *testing.T) {
	payload, err := os.ReadFile("testdata/live-search.json")
	if err != nil {
		t.Fatal(err)
	}
	products, err := parseLiveSearch(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) == 0 || products[0].ID != "16031" || products[0].Name != "Sauvage" || products[0].Edition != "2015" {
		t.Fatalf("first=%+v", products[0])
	}
	if products[0].URL != "https://xn--d1ai6ai.xn--p1ai/catalog/men/Christian-Dior/Sauvage-2015" {
		t.Fatal(products[0].URL)
	}
}

func TestParseProductSeparatesKinds(t *testing.T) {
	f, err := os.Open("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	product := productLink{ID: "16031", Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", URL: "https://xn--d1ai6ai.xn--p1ai/catalog/men/Christian-Dior/Sauvage-2015"}
	offers, err := parseProduct(f, product, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	assertDuhiOffer(t, offers, domain.ProductKindSample, 1500, 57200)
	assertDuhiOffer(t, offers, domain.ProductKindTester, 100000, 1166500)
	assertDuhiOffer(t, offers, domain.ProductKindRetail, 100000, 1343400)
}

func assertDuhiOffer(t *testing.T, offers []domain.Offer, kind domain.ProductKind, volume int, price int64) {
	t.Helper()
	for _, o := range offers {
		if o.Kind == kind && o.VolumeMicroliters == volume && o.PriceKopecks == price {
			return
		}
	}
	t.Fatalf("offer %s/%d/%d absent", kind, volume, price)
}
