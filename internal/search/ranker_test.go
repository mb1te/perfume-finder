package search

import (
	"reflect"
	"testing"

	"parfumes_finder/internal/domain"
)

func TestGroupAndSortKeepsKindsSeparateAndPricesAscending(t *testing.T) {
	t.Parallel()

	query := domain.SearchQuery{
		Brand:             "Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindAll,
	}
	offers := []domain.Offer{
		offer("retail-b", domain.ProductKindRetail, 1200000, true),
		offer("tester", domain.ProductKindTester, 900000, true),
		offer("retail-a", domain.ProductKindRetail, 1100000, true),
		offer("zero", domain.ProductKindRetail, 0, true),
		offer("sold-out", domain.ProductKindRetail, 1000000, false),
		offer("unknown", domain.ProductKindUnknown, 100, true),
	}

	groups := GroupAndSort(query, offers)

	if got, want := shopIDs(groups[domain.ProductKindRetail]), []string{"retail-a", "retail-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retail order = %v, want %v", got, want)
	}
	if got, want := shopIDs(groups[domain.ProductKindTester]), []string{"tester"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tester order = %v, want %v", got, want)
	}
	if _, exists := groups[domain.ProductKindUnknown]; exists {
		t.Fatal("unknown offers must not be ranked")
	}
}

func TestGroupAndSortBreaksPriceTiesByShopID(t *testing.T) {
	t.Parallel()

	query := domain.SearchQuery{
		Brand:             "Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindRetail,
	}
	offers := []domain.Offer{
		offer("randewoo", domain.ProductKindRetail, 1200000, true),
		offer("orental", domain.ProductKindRetail, 1200000, true),
	}

	groups := GroupAndSort(query, offers)
	if got, want := shopIDs(groups[domain.ProductKindRetail]), []string{"orental", "randewoo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tie order = %v, want %v", got, want)
	}
}

func offer(shopID string, kind domain.ProductKind, price int64, inStock bool) domain.Offer {
	return domain.Offer{
		ShopID:            shopID,
		Brand:             "Christian Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              kind,
		PriceKopecks:      price,
		InStock:           inStock,
	}
}

func shopIDs(offers []domain.Offer) []string {
	ids := make([]string, len(offers))
	for index, offer := range offers {
		ids[index] = offer.ShopID
	}
	return ids
}
