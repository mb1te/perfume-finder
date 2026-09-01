package telegram

import (
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"strings"
	"testing"
	"time"
)

func TestRenderResultRejectsUnsafeURL(t *testing.T) {
	q := domain.SearchQuery{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}
	result := search.Result{Offers: []domain.Offer{{ShopID: "shop", Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 100, InStock: true, URL: "javascript:alert(1)"}}}
	got := RenderResult(q, result)
	if strings.Contains(got, "javascript:") {
		t.Fatal(got)
	}
}

func TestRenderResultKeepsApostropheInPlainText(t *testing.T) {
	query := domain.SearchQuery{
		Brand:             "Kilian",
		Name:              "Angel's Share",
		Concentration:     domain.ConcentrationEDP,
		VolumeMicroliters: 30000,
		Kind:              domain.ProductKindRetail,
	}

	got := RenderResult(query, search.Result{})
	wantPrefix := "Kilian Angel's Share · edp · 30 мл\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("result = %q, want prefix %q", got, wantPrefix)
	}
}

func TestRenderResultExplainsNoExactMatchAndShowsCheckTime(t *testing.T) {
	query := domain.SearchQuery{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}
	if got := RenderResult(query, search.Result{}); !strings.Contains(got, "Точный вариант не найден") {
		t.Fatal(got)
	}
	offer := domain.Offer{ShopID: "shop", Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 100, InStock: true, URL: "https://shop.test", RetrievedAt: time.Date(2026, 8, 31, 12, 34, 0, 0, time.UTC)}
	if got := RenderResult(query, search.Result{Offers: []domain.Offer{offer}}); !strings.Contains(got, "В наличии") || !strings.Contains(got, "12:34") {
		t.Fatal(got)
	}
}
