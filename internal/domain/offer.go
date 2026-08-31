package domain

import "time"

type Offer struct {
	ShopID            string
	RawTitle          string
	Brand             string
	Name              string
	Edition           string
	Concentration     Concentration
	VolumeMicroliters int
	Kind              ProductKind
	PriceKopecks      int64
	InStock           bool
	URL               string
	RetrievedAt       time.Time
}
