package orental

import (
	"os"
	"testing"
	"time"

	"parfumes_finder/internal/domain"
)

func TestParseSearchKeepsSauvageSeparateFromEauSauvage(t *testing.T) {
	file, err := os.Open("testdata/search-dior-sauvage.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	products, err := parseSearch(file, "https://www.orental.ru")
	if err != nil {
		t.Fatal(err)
	}
	plain, eau := false, false
	for _, product := range products {
		plain = plain || product.Name == "Sauvage" && product.URL == "https://www.orental.ru/men/christian-dior/sauvage/"
		eau = eau || product.Name == "Eau Sauvage"
	}
	if !plain || !eau {
		t.Fatalf("plain=%v eau=%v products=%+v", plain, eau, products)
	}
}

func TestParseProductUsesExactJSONLDVariants(t *testing.T) {
	file, err := os.Open("testdata/product-sauvage.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	offers, err := parseProduct(file, productLink{Brand: "Christian Dior", Name: "Sauvage", URL: "https://www.orental.ru/men/christian-dior/sauvage/"}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertVariant(t, offers, domain.ConcentrationEDT, domain.ProductKindRetail, 100000, 1402800)
	assertVariant(t, offers, domain.ConcentrationEDT, domain.ProductKindTester, 100000, 1281500)
	assertVariant(t, offers, domain.ConcentrationEDT, domain.ProductKindSample, 3000, 115400)
	for _, offer := range offers {
		if offer.Kind == domain.ProductKindUnknown {
			t.Fatalf("unknown offer leaked: %+v", offer)
		}
	}
}

func assertVariant(t *testing.T, offers []domain.Offer, concentration domain.Concentration, kind domain.ProductKind, volume int, price int64) {
	t.Helper()
	for _, offer := range offers {
		if offer.Concentration == concentration && offer.Kind == kind && offer.VolumeMicroliters == volume && offer.PriceKopecks == price {
			return
		}
	}
	t.Fatalf("variant concentration=%q kind=%q volume=%d price=%d not found", concentration, kind, volume, price)
}
