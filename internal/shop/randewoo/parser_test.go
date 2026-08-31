package randewoo

import (
	"os"
	"parfumes_finder/internal/domain"
	"testing"
	"time"
)

func TestParseAutocompleteKeepsPerfumeAndRejectsShowerGel(t *testing.T) {
	payload, err := os.ReadFile("testdata/autocomplete-dior-sauvage.json")
	if err != nil {
		t.Fatal(err)
	}
	products, err := parseAutocomplete(payload)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, product := range products {
		if product.ID == "454160" {
			t.Fatal("shower gel leaked")
		}
		found = found || product.ID == "437349" && product.Name == "Sauvage" && product.Edition == "2015"
	}
	if !found {
		t.Fatal("Sauvage 2015 perfume candidate absent")
	}
}

func TestParseProductReturnsSelectedOfferAndSKUList(t *testing.T) {
	f, err := os.Open("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	product := productLink{ID: "437349", Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", URL: "https://randewoo.ru/product/christian-dior-sauvage-2015?preferred=437349"}
	offer, skus, err := parseProduct(f, product, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if offer.Kind != domain.ProductKindSample || offer.VolumeMicroliters != 1500 || offer.PriceKopecks != 34500 {
		t.Fatalf("offer=%+v", offer)
	}
	if len(skus) < 10 || skus[0] != "437349" {
		t.Fatalf("skus=%v", skus)
	}
}
