package search

import "parfumes_finder/internal/domain"

var brandAliases = map[string]string{
	"dior":           "christian dior",
	"christian dior": "christian dior",
}

func SameVariant(query domain.SearchQuery, offer domain.Offer) bool {
	if canonicalBrand(query.Brand) != canonicalBrand(offer.Brand) {
		return false
	}
	if domain.NormalizeText(query.Name) != domain.NormalizeText(offer.Name) {
		return false
	}
	if domain.NormalizeText(query.Edition) != domain.NormalizeText(offer.Edition) {
		return false
	}
	if query.Concentration != offer.Concentration {
		return false
	}
	if query.VolumeMicroliters != offer.VolumeMicroliters {
		return false
	}
	if query.Kind == domain.ProductKindAll {
		return isKnownKind(offer.Kind)
	}
	return isKnownKind(query.Kind) && query.Kind == offer.Kind
}

func canonicalBrand(value string) string {
	normalized := domain.NormalizeText(value)
	if canonical, ok := brandAliases[normalized]; ok {
		return canonical
	}
	return normalized
}

func isKnownKind(kind domain.ProductKind) bool {
	switch kind {
	case domain.ProductKindRetail,
		domain.ProductKindTester,
		domain.ProductKindDecant,
		domain.ProductKindMiniature,
		domain.ProductKindSample:
		return true
	default:
		return false
	}
}
