package allure

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

const allureBaseURL = "https://allureparfum.ru"

type Adapter struct {
	client  *httpx.Client
	clock   func() time.Time
	baseURL string
}

func New(client *httpx.Client, clock func() time.Time) shop.Adapter {
	return newAdapter(client, clock, allureBaseURL)
}

func newAdapter(client *httpx.Client, clock func() time.Time, baseURL string) *Adapter {
	return &Adapter{client: client, clock: clock, baseURL: strings.TrimRight(baseURL, "/")}
}

func (*Adapter) ID() string {
	return "allure"
}

func (adapter *Adapter) Search(ctx context.Context, query domain.SearchQuery) ([]domain.Offer, error) {
	searchText := strings.TrimSpace(query.Raw)
	if searchText == "" {
		searchText = strings.TrimSpace(query.Brand + " " + query.Name + " " + query.Edition)
	}
	searchURL := adapter.baseURL + "/search/?" + url.Values{"q": {searchText}}.Encode()
	document, err := adapter.client.GetDocument(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	products, err := parseSearchDocument(document, adapter.baseURL)
	if err != nil {
		return nil, err
	}

	var offers []domain.Offer
	var lastErr error
	for _, product := range products {
		if !matchesSelectedProduct(query, product) {
			continue
		}
		if validateErr := httpx.ValidateURLHost(product.URL, adapter.baseURL); validateErr != nil {
			lastErr = shop.NewError(shop.ErrorAccess, validateErr)
			continue
		}
		productDocument, getErr := adapter.client.GetDocument(ctx, product.URL)
		if getErr != nil {
			lastErr = getErr
			continue
		}
		productOffers, parseErr := parseProductDocument(productDocument, product, adapter.clock())
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		offers = append(offers, productOffers...)
	}
	if len(offers) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return offers, nil
}

func (adapter *Adapter) Health(ctx context.Context) shop.HealthResult {
	checkedAt := adapter.clock()
	searchURL := adapter.baseURL + "/search/?" + url.Values{"q": {"Dior Sauvage"}}.Encode()
	document, err := adapter.client.GetDocument(ctx, searchURL)
	if err != nil {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checkedAt}
	}
	products, err := parseSearchDocument(document, adapter.baseURL)
	if err != nil || len(products) == 0 {
		if err == nil {
			err = shop.NewError(shop.ErrorParse, fmt.Errorf("Allure search returned no products"))
		}
		return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checkedAt}
	}
	return shop.HealthResult{Status: shop.HealthHealthy, CanonicalURL: adapter.baseURL, CheckedAt: checkedAt}
}

func matchesSelectedProduct(query domain.SearchQuery, product productLink) bool {
	if query.Brand != "" && domain.NormalizeText(query.Brand) != domain.NormalizeText(product.Brand) && domain.NormalizeText(query.Brand) != "dior" {
		return false
	}
	if query.Name != "" && domain.NormalizeText(query.Name) != domain.NormalizeText(product.Name) {
		return false
	}
	if query.Edition != "" && domain.NormalizeText(query.Edition) != domain.NormalizeText(product.Edition) {
		return false
	}
	return true
}
