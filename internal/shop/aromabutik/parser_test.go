package aromabutik

import (
	"os"
	"parfumes_finder/internal/domain"
	"testing"
	"time"
)

func TestParseSearchExcludesInspiredByProduct(t *testing.T) {
	f, err := os.Open("testdata/search-dior-sauvage.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	products, err := parseSearch(f, "https://www.aroma-butik.ru")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, product := range products {
		if product.Brand == "Areej" {
			t.Fatalf("inspired-by product leaked: %+v", product)
		}
		found = found || product.Brand == "Christian Dior" && product.Name == "Sauvage" && product.Edition == "2015"
	}
	if !found {
		t.Fatal("Christian Dior Sauvage 2015 not found")
	}
}

func TestParseProductReadsMicrodataVariants(t *testing.T) {
	f, err := os.Open("testdata/product-sauvage-2015.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	product := productLink{"Christian Dior", "Sauvage", "2015", "https://www.aroma-butik.ru/product/christian-dior-sauvage-2015/"}
	offers, err := parseProduct(f, product, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	assertAromaOffer(t, offers, domain.ProductKindSample, 2000, 24000)
	assertAromaOffer(t, offers, domain.ProductKindTester, 100000, 1223000)
}

func TestParseDisplayedRublesTreatsDotAsThousandsSeparator(t *testing.T) {
	if got := parseDisplayedRubles("29.600 ₽"); got != 29600 {
		t.Fatalf("got %d", got)
	}
}

func assertAromaOffer(t *testing.T, offers []domain.Offer, kind domain.ProductKind, volume int, price int64) {
	t.Helper()
	for _, offer := range offers {
		if offer.Kind == kind && offer.VolumeMicroliters == volume && offer.PriceKopecks == price {
			return
		}
	}
	t.Fatalf("offer %s/%d/%d not found", kind, volume, price)
}
