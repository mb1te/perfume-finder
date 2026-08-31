package domain

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
