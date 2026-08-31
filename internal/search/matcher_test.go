package search

import (
	"testing"

	"parfumes_finder/internal/domain"
)

func TestSameVariantRejectsConfusingProducts(t *testing.T) {
	t.Parallel()

	query := domain.SearchQuery{
		Brand:             "Christian Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindRetail,
	}
	base := domain.Offer{
		Brand:             "Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindRetail,
	}

	tests := []struct {
		name   string
		mutate func(*domain.Offer)
		want   bool
	}{
		{name: "exact with brand alias", mutate: func(*domain.Offer) {}, want: true},
		{name: "eau sauvage", mutate: func(offer *domain.Offer) { offer.Name = "Eau Sauvage" }, want: false},
		{name: "elixir", mutate: func(offer *domain.Offer) { offer.Concentration = domain.ConcentrationElixir }, want: false},
		{name: "other edition", mutate: func(offer *domain.Offer) { offer.Edition = "2025" }, want: false},
		{name: "sample", mutate: func(offer *domain.Offer) { offer.Kind = domain.ProductKindSample }, want: false},
		{name: "other volume", mutate: func(offer *domain.Offer) { offer.VolumeMicroliters = 60000 }, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offer := base
			tt.mutate(&offer)
			if got := SameVariant(query, offer); got != tt.want {
				t.Fatalf("SameVariant() = %v, want %v for %s", got, tt.want, tt.name)
			}
		})
	}
}

func TestSameVariantAllowsKnownKindsForAllKindsQuery(t *testing.T) {
	t.Parallel()

	query := domain.SearchQuery{
		Brand:             "Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindAll,
	}
	offer := domain.Offer{
		Brand:             "Christian Dior",
		Name:              "Sauvage",
		Edition:           "2015",
		Concentration:     domain.ConcentrationEDT,
		VolumeMicroliters: 100000,
		Kind:              domain.ProductKindTester,
	}

	if !SameVariant(query, offer) {
		t.Fatal("all-kinds query rejected a known product kind")
	}

	offer.Kind = domain.ProductKindUnknown
	if SameVariant(query, offer) {
		t.Fatal("all-kinds query accepted an unknown product kind")
	}
}

func TestSameVariantRequiresEditionEqualityEvenWhenOneSideIsEmpty(t *testing.T) {
	query := domain.SearchQuery{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}
	offer := domain.Offer{Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}
	if SameVariant(query, offer) {
		t.Fatal("unspecified query edition accepted a year-specific offer")
	}
	query.Edition = "2015"
	if !SameVariant(query, offer) {
		t.Fatal("matching explicit edition rejected")
	}
	query.Edition = "2025"
	if SameVariant(query, offer) {
		t.Fatal("explicit different edition was accepted")
	}
}
