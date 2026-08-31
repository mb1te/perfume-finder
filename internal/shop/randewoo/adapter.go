package randewoo

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/shop/httpx"
	"strings"
	"time"
)

const apiEndpoint = "https://autocomplete.diginetica.net/autocomplete"
const apiKey = "594L68C4CP"
const siteURL = "https://randewoo.ru"

type Adapter struct {
	client          *httpx.Client
	clock           func() time.Time
	apiURL, siteURL string
}

func New(c *httpx.Client, clock func() time.Time) shop.Adapter {
	return newAdapter(c, clock, apiEndpoint, siteURL)
}
func newAdapter(c *httpx.Client, clock func() time.Time, api, site string) *Adapter {
	return &Adapter{c, clock, api, strings.TrimRight(site, "/")}
}
func (*Adapter) ID() string { return "randewoo" }

func (a *Adapter) candidates(ctx context.Context, text string) ([]productLink, error) {
	payload, err := a.client.Get(ctx, a.apiURL+"?"+url.Values{"st": {text}, "apiKey": {apiKey}}.Encode())
	if err != nil {
		return nil, err
	}
	return parseAutocomplete(payload)
}
func (a *Adapter) Search(ctx context.Context, q domain.SearchQuery) ([]domain.Offer, error) {
	text := strings.TrimSpace(q.Raw)
	if text == "" {
		text = strings.TrimSpace(q.Brand + " " + q.Name)
	}
	products, err := a.candidates(ctx, text)
	if err != nil {
		return nil, err
	}
	var offers []domain.Offer
	seen := map[string]bool{}
	for _, p := range products {
		if q.Name != "" && domain.NormalizeText(q.Name) != domain.NormalizeText(p.Name) {
			continue
		}
		payload, e := a.client.Get(ctx, p.URL)
		if e != nil {
			continue
		}
		offer, skus, e := parseProduct(bytes.NewReader(payload), p, a.clock())
		if e == nil && offer.InStock && !seen[offer.URL] {
			offers = append(offers, offer)
			seen[offer.URL] = true
		}
		for _, sku := range skus {
			if sku == p.ID {
				continue
			}
			u, parseErr := url.Parse(p.URL)
			if parseErr != nil {
				continue
			}
			params := u.Query()
			params.Set("preferred", sku)
			u.RawQuery = params.Encode()
			variantPayload, getErr := a.client.Get(ctx, u.String())
			if getErr != nil {
				continue
			}
			variant, _, parseErr := parseProduct(bytes.NewReader(variantPayload), p, a.clock())
			if parseErr == nil && variant.InStock && !seen[variant.URL] {
				offers = append(offers, variant)
				seen[variant.URL] = true
			}
		}
	}
	return offers, nil
}
func (a *Adapter) Health(ctx context.Context) shop.HealthResult {
	checked := a.clock()
	products, err := a.candidates(ctx, "Dior Sauvage")
	if err == nil && len(products) == 0 {
		err = fmt.Errorf("Randewoo autocomplete returned no perfume products")
	}
	if err != nil {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checked}
	}
	return shop.HealthResult{Status: shop.HealthHealthy, CanonicalURL: a.siteURL, CheckedAt: checked}
}
