package duhirf

import (
	"context"
	"fmt"
	"net/url"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/shop/httpx"
	"strings"
	"time"
)

const duhiBaseURL = "https://xn--d1ai6ai.xn--p1ai"

type Adapter struct {
	client  *httpx.Client
	clock   func() time.Time
	baseURL string
}

func New(client *httpx.Client, clock func() time.Time) shop.Adapter {
	return newAdapter(client, clock, duhiBaseURL)
}
func newAdapter(client *httpx.Client, clock func() time.Time, root string) *Adapter {
	return &Adapter{client, clock, strings.TrimRight(root, "/")}
}
func (*Adapter) ID() string { return "duhirf" }

func (a *Adapter) searchLinks(ctx context.Context, text string) ([]productLink, error) {
	payload, err := a.client.PostForm(ctx, a.baseURL+"/index.php?section=6", url.Values{"cmd": {"live_search"}, "text": {strings.ToLower(text)}})
	if err != nil {
		return nil, err
	}
	products, err := parseLiveSearch(payload)
	if err != nil {
		return nil, err
	}
	for i := range products {
		if strings.HasPrefix(products[i].URL, "/") && !strings.HasPrefix(products[i].URL, "//") {
			products[i].URL = a.baseURL + products[i].URL
		}
	}
	return products, nil
}
func (a *Adapter) Search(ctx context.Context, q domain.SearchQuery) ([]domain.Offer, error) {
	text := strings.TrimSpace(q.Raw)
	if text == "" {
		text = strings.TrimSpace(q.Brand + " " + q.Name)
	}
	products, err := a.searchLinks(ctx, text)
	if err != nil {
		return nil, err
	}
	var offers []domain.Offer
	var last error
	for _, p := range products {
		if q.Name != "" && domain.NormalizeText(q.Name) != domain.NormalizeText(p.Name) {
			continue
		}
		if validateErr := httpx.ValidateURLHost(p.URL, a.baseURL); validateErr != nil {
			last = shop.NewError(shop.ErrorAccess, validateErr)
			continue
		}
		doc, e := a.client.GetDocument(ctx, p.URL)
		if e != nil {
			last = e
			continue
		}
		parsed, e := parseProductDocument(doc, p, a.clock())
		if e != nil {
			last = e
			continue
		}
		offers = append(offers, parsed...)
	}
	if len(offers) == 0 && last != nil {
		return nil, last
	}
	return offers, nil
}
func (a *Adapter) Health(ctx context.Context) shop.HealthResult {
	checked := a.clock()
	products, err := a.searchLinks(ctx, "Dior Sauvage")
	if err == nil && len(products) == 0 {
		err = fmt.Errorf("Duhi search returned no products")
	}
	if err != nil {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: shop.NewError(shop.ErrorParse, err), CheckedAt: checked}
	}
	return shop.HealthResult{Status: shop.HealthHealthy, CanonicalURL: a.baseURL, CheckedAt: checked}
}
