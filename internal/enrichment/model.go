package enrichment

import (
	"context"
	"errors"
	"time"

	"parfumes_finder/internal/domain"
)

var ErrImageTooLarge = errors.New("enrichment image exceeds 5 MiB")

type Request struct {
	Brand, Name, Edition string
	Concentration        domain.Concentration
}

type Accord struct {
	Name, Color string
	Width       int
}

type Card struct {
	SourceURL   string
	Title       string
	Accords     []Accord
	PNG         []byte
	RetrievedAt time.Time
}

type Service interface {
	Enrich(context.Context, Request) (Card, bool, error)
}
