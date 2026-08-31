package telegram

import (
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"strings"
	"testing"
)

func TestRenderResultEscapesTextAndRejectsUnsafeURL(t *testing.T) {
	q := domain.SearchQuery{Brand: "<b>Dior</b>", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}
	result := search.Result{Offers: []domain.Offer{{ShopID: "<script>", Brand: "<b>Dior</b>", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 100, InStock: true, URL: "javascript:alert(1)"}}}
	got := RenderResult(q, result)
	if strings.Contains(got, "<script>") || strings.Contains(got, "javascript:") {
		t.Fatal(got)
	}
}
