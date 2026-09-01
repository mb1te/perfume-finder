package domain

import "sort"

type ProductKind string

const (
	ProductKindUnknown   ProductKind = "unknown"
	ProductKindRetail    ProductKind = "retail"
	ProductKindTester    ProductKind = "tester"
	ProductKindDecant    ProductKind = "decant"
	ProductKindMiniature ProductKind = "miniature"
	ProductKindSample    ProductKind = "sample"
	ProductKindAll       ProductKind = "all"
)

type Concentration string

const (
	ConcentrationUnknown Concentration = "unknown"
	ConcentrationEDT     Concentration = "edt"
	ConcentrationEDP     Concentration = "edp"
	ConcentrationParfum  Concentration = "parfum"
	ConcentrationExtrait Concentration = "extrait"
	ConcentrationCologne Concentration = "cologne"
	ConcentrationElixir  Concentration = "elixir"
)

type SearchQuery struct {
	Raw               string
	Brand             string
	Name              string
	Edition           string
	Concentration     Concentration
	VolumeMicroliters int
	Kind              ProductKind
}

type FragranceCandidate struct {
	Query          SearchQuery
	Concentrations []Concentration
}

func SortConcentrations(values []Concentration) []Concentration {
	order := map[Concentration]int{
		ConcentrationEDT:     0,
		ConcentrationEDP:     1,
		ConcentrationParfum:  2,
		ConcentrationExtrait: 3,
		ConcentrationCologne: 4,
		ConcentrationElixir:  5,
	}
	result := append([]Concentration(nil), values...)
	sort.Slice(result, func(i, j int) bool { return order[result[i]] < order[result[j]] })
	return result
}
