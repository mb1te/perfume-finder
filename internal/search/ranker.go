package search

import (
	"sort"

	"parfumes_finder/internal/domain"
)

func GroupAndSort(query domain.SearchQuery, offers []domain.Offer) map[domain.ProductKind][]domain.Offer {
	groups := make(map[domain.ProductKind][]domain.Offer)
	for _, offer := range offers {
		if !offer.InStock || offer.PriceKopecks <= 0 || !SameVariant(query, offer) {
			continue
		}
		groups[offer.Kind] = append(groups[offer.Kind], offer)
	}

	for kind := range groups {
		sort.SliceStable(groups[kind], func(left, right int) bool {
			if groups[kind][left].PriceKopecks == groups[kind][right].PriceKopecks {
				return groups[kind][left].ShopID < groups[kind][right].ShopID
			}
			return groups[kind][left].PriceKopecks < groups[kind][right].PriceKopecks
		})
	}

	return groups
}
