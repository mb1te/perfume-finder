package randewoo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

var randewooYear = regexp.MustCompile(`\s*\(?(19\d{2}|20\d{2})\)?\s*$`)
var skuListPattern = regexp.MustCompile(`"skus":(\[[^\]]+\])`)

type productLink struct{ ID, Brand, Name, Edition, URL string }

func parseAutocomplete(payload []byte) ([]productLink, error) {
	var response struct {
		Products []struct {
			ID         string `json:"id"`
			Available  bool   `json:"available"`
			Name       string `json:"name"`
			Brand      string `json:"brand"`
			LinkURL    string `json:"link_url"`
			Categories []struct {
				Name string `json:"name"`
			} `json:"categories"`
		} `json:"products"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	var products []productLink
	for _, p := range response.Products {
		if !p.Available {
			continue
		}
		perfume := false
		for _, c := range p.Categories {
			if strings.Contains(domain.NormalizeText(c.Name), "парфюмерия") {
				perfume = true
			}
		}
		if !perfume {
			continue
		}
		name, edition := p.Name, ""
		if m := randewooYear.FindStringSubmatch(name); len(m) == 2 {
			edition = m[1]
			name = strings.TrimSpace(randewooYear.ReplaceAllString(name, ""))
		}
		products = append(products, productLink{p.ID, p.Brand, name, edition, p.LinkURL})
	}
	return products, nil
}

func parseProduct(reader io.Reader, product productLink, now time.Time) (domain.Offer, []string, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return domain.Offer{}, nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(payload))
	if err != nil {
		return domain.Offer{}, nil, err
	}
	type schemaOffer struct {
		Availability string `json:"availability"`
		Price        int64  `json:"price"`
		SKU          string `json:"sku"`
	}
	type schemaProduct struct {
		Type   string        `json:"@type"`
		Name   string        `json:"name"`
		URL    string        `json:"url"`
		Offers []schemaOffer `json:"offers"`
	}
	var schema schemaProduct
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var candidate schemaProduct
		if json.Unmarshal([]byte(s.Text()), &candidate) == nil && candidate.Type == "Product" && len(candidate.Offers) > 0 {
			schema = candidate
			return false
		}
		return true
	})
	if len(schema.Offers) == 0 {
		return domain.Offer{}, nil, shop.NewError(shop.ErrorParse, fmt.Errorf("Randewoo product JSON-LD missing"))
	}
	selected := schema.Offers[0]
	concentration := domain.ParseConcentration(schema.Name)
	volume := domain.ParseVolumeMicroliters(schema.Name)
	kind := domain.ClassifyKind(doc.Find("title").Text())
	if kind == domain.ProductKindUnknown && concentration != domain.ConcentrationUnknown {
		kind = domain.ProductKindRetail
	}
	if concentration == domain.ConcentrationUnknown || volume == 0 || kind == domain.ProductKindUnknown || selected.Price <= 0 {
		return domain.Offer{}, nil, shop.NewError(shop.ErrorParse, fmt.Errorf("Randewoo selected SKU is incomplete"))
	}
	var skus []string
	if match := skuListPattern.FindSubmatch(payload); len(match) == 2 {
		_ = json.Unmarshal(match[1], &skus)
	}
	offer := domain.Offer{ShopID: "randewoo", RawTitle: schema.Name, Brand: product.Brand, Name: product.Name, Edition: product.Edition, Concentration: concentration, VolumeMicroliters: volume, Kind: kind, PriceKopecks: selected.Price * 100, InStock: strings.Contains(selected.Availability, "InStock"), URL: schema.URL, RetrievedAt: now}
	return offer, skus, nil
}
