package orental

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

type productLink struct{ Brand, Name, URL string }

func parseSearch(reader io.Reader, baseURL string) ([]productLink, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	return parseSearchDocument(doc, baseURL)
}

func parseSearchDocument(doc *goquery.Document, baseURL string) ([]productLink, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	var products []productLink
	doc.Find(".prod-teaser").Each(func(_ int, card *goquery.Selection) {
		link := card.Find("a.prod-teaser__title").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		resolved, parseErr := base.Parse(href)
		if parseErr != nil {
			return
		}
		brand := strings.TrimSpace(link.Find(".prod-teaser__brand").Text())
		name := strings.TrimSpace(link.Find(".prod-teaser__name").Text())
		if brand != "" && name != "" {
			products = append(products, productLink{brand, name, resolved.String()})
		}
	})
	return products, nil
}

func parseProduct(reader io.Reader, product productLink, now time.Time) ([]domain.Offer, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	return parseProductDocument(doc, product, now)
}

func parseProductDocument(doc *goquery.Document, product productLink, now time.Time) ([]domain.Offer, error) {
	type schemaOffer struct {
		Item         string `json:"item"`
		Price        int64  `json:"price"`
		Availability string `json:"availability"`
	}
	type schemaProduct struct {
		Type   string `json:"@type"`
		Offers struct {
			Offers []schemaOffer `json:"offers"`
		} `json:"offers"`
	}
	var schema schemaProduct
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, script *goquery.Selection) bool {
		var candidate schemaProduct
		if json.Unmarshal([]byte(script.Text()), &candidate) == nil && candidate.Type == "Product" && len(candidate.Offers.Offers) > 0 {
			schema = candidate
			return false
		}
		return true
	})
	if len(schema.Offers.Offers) == 0 {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("Orental product offers JSON-LD not found"))
	}
	var offers []domain.Offer
	for _, item := range schema.Offers.Offers {
		concentration := domain.ParseConcentration(item.Item)
		volume := domain.ParseVolumeMicroliters(item.Item)
		kind := domain.ClassifyKind(item.Item)
		normalized := domain.NormalizeText(item.Item)
		if kind == domain.ProductKindUnknown && concentration != domain.ConcentrationUnknown && !strings.Contains(normalized, "refill") && !strings.Contains(normalized, "+") {
			kind = domain.ProductKindRetail
		}
		if concentration == domain.ConcentrationUnknown || volume == 0 || kind == domain.ProductKindUnknown || item.Price <= 0 || !strings.Contains(item.Availability, "InStock") {
			continue
		}
		offers = append(offers, domain.Offer{ShopID: "orental", RawTitle: item.Item, Brand: product.Brand, Name: product.Name, Concentration: concentration, VolumeMicroliters: volume, Kind: kind, PriceKopecks: item.Price * 100, InStock: true, URL: product.URL, RetrievedAt: now})
	}
	return offers, nil
}
