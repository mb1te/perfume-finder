package allure

import (
	"os"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
)

func TestParseSearchFindsSauvage2015(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/search-dior-sauvage.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	products, err := parseSearch(file, "https://allureparfum.ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 11 {
		t.Fatalf("product count = %d, want 11", len(products))
	}
	product, ok := findProduct(products, "Christian Dior", "Sauvage", "2015")
	if !ok {
		t.Fatal("Christian Dior Sauvage 2015 not found")
	}
	if product.URL != "https://allureparfum.ru/katalog/muzhskaya-parfyumeriya/christian-dior/sauvage-2015.html" {
		t.Fatalf("product URL = %q", product.URL)
	}
}

func TestParseProductEmitsVerifiedSampleAndRetailVariants(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	retrievedAt := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	product := productLink{
		Brand:   "Christian Dior",
		Name:    "Sauvage",
		Edition: "2015",
		URL:     "https://allureparfum.ru/katalog/muzhskaya-parfyumeriya/christian-dior/sauvage-2015.html",
	}
	offers, err := parseProduct(file, product, retrievedAt)
	if err != nil {
		t.Fatal(err)
	}

	assertOffer(t, offers, domain.ProductKindSample, 1000, 28000)
	assertOffer(t, offers, domain.ProductKindRetail, 100000, 1504500)
	for _, offer := range offers {
		if offer.Kind == domain.ProductKindUnknown {
			t.Fatalf("unknown offer leaked into results: %+v", offer)
		}
		if offer.Concentration != domain.ConcentrationEDT {
			t.Fatalf("concentration = %q, want EDT", offer.Concentration)
		}
		if offer.RetrievedAt != retrievedAt {
			t.Fatalf("RetrievedAt = %v", offer.RetrievedAt)
		}
	}
}

func findProduct(products []productLink, brand, name, edition string) (productLink, bool) {
	for _, product := range products {
		if product.Brand == brand && product.Name == name && product.Edition == edition {
			return product, true
		}
	}
	return productLink{}, false
}

func assertOffer(t *testing.T, offers []domain.Offer, kind domain.ProductKind, volume int, price int64) {
	t.Helper()

	for _, offer := range offers {
		if offer.Kind == kind && offer.VolumeMicroliters == volume && offer.PriceKopecks == price {
			return
		}
	}
	t.Fatalf("offer kind=%q volume=%d price=%d not found", kind, volume, price)
}
