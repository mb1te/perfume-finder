package aromabutik

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/shop/httpx"
)

const aromaBaseURL = "https://www.aroma-butik.ru"

type Adapter struct {
	client  *httpx.Client
	clock   func() time.Time
	baseURL string
}

func New(client *httpx.Client, clock func() time.Time) shop.Adapter {
	return newAdapter(client, clock, aromaBaseURL)
}
func newAdapter(client *httpx.Client, clock func() time.Time, root string) *Adapter {
	return &Adapter{client, clock, strings.TrimRight(root, "/")}
}
func (*Adapter) ID() string { return "aromabutik" }

func (a *Adapter) Search(ctx context.Context, query domain.SearchQuery) ([]domain.Offer, error) {
	text := strings.TrimSpace(query.Raw)
	if text == "" {
		text = strings.TrimSpace(query.Brand + " " + query.Name)
	}
	doc, err := a.client.GetDocument(ctx, a.baseURL+"/advanced_search_result.php?"+url.Values{"keywords": {text}}.Encode())
	if err != nil {
		return nil, err
	}
	products, err := parseSearchDocument(doc, a.baseURL)
	if err != nil {
		return nil, err
	}
	var offers []domain.Offer
	var lastErr error
	for _, product := range products {
		if query.Name != "" && domain.NormalizeText(query.Name) != domain.NormalizeText(product.Name) {
			continue
		}
		pd, getErr := a.client.GetDocument(ctx, product.URL)
		if getErr != nil {
			lastErr = getErr
			continue
		}
		parsed, parseErr := parseProductDocument(pd, product, a.clock())
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		offers = append(offers, parsed...)
	}
	if len(offers) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return offers, nil
}

func (a *Adapter) Health(ctx context.Context) shop.HealthResult {
	checked := a.clock()
	doc, err := a.client.GetDocument(ctx, a.baseURL+"/advanced_search_result.php?keywords=Dior+Sauvage")
	if err == nil {
		var products []productLink
		products, err = parseSearchDocument(doc, a.baseURL)
		if len(products) == 0 && err == nil {
			err = fmt.Errorf("Aroma Butik search returned no products")
		}
	}
	if err != nil {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: shop.NewError(shop.ErrorParse, err), CheckedAt: checked}
	}
	return shop.HealthResult{Status: shop.HealthHealthy, CanonicalURL: a.baseURL, CheckedAt: checked}
}
