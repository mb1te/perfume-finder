package duhirf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/shop"
)

var yearSuffix = regexp.MustCompile(`\s*\(?(19\d{2}|20\d{2})\)?\s*$`)
var onlyDigits = regexp.MustCompile(`\D`)

type productLink struct{ ID, Brand, Name, Edition, URL string }

func parseLiveSearch(payload []byte) ([]productLink, error) {
	var html string
	if err := json.Unmarshal(payload, &html); err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("decode Duhi search JSON: %w", err))
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewBufferString(html))
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	var products []productLink
	doc.Find("a.term_res_prod").Each(func(_ int, link *goquery.Selection) {
		model, _ := link.Attr("data-model")
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		parts := strings.SplitN(model, " - ", 2)
		if len(parts) != 2 {
			return
		}
		name, edition := parts[1], ""
		if match := yearSuffix.FindStringSubmatch(name); len(match) == 2 {
			edition = match[1]
			name = strings.TrimSpace(yearSuffix.ReplaceAllString(name, ""))
		}
		if strings.HasPrefix(href, "//") {
			href = "https:" + href
		}
		id, _ := link.Attr("data-product")
		products = append(products, productLink{id, strings.TrimSpace(parts[0]), strings.TrimSpace(name), edition, href})
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
	base, err := url.Parse(product.URL)
	if err != nil {
		return nil, err
	}
	var offers []domain.Offer
	doc.Find(`tr[itemprop="offers"]`).Each(func(_ int, row *goquery.Selection) {
		title := strings.TrimSpace(row.Find(`[itemprop="name"]`).First().Text())
		concentration := domain.ParseConcentration(title)
		volume := domain.ParseVolumeMicroliters(title)
		kind := domain.ClassifyKind(row.Text())
		if kind == domain.ProductKindUnknown && concentration != domain.ConcentrationUnknown {
			kind = domain.ProductKindRetail
		}
		priceText := onlyDigits.ReplaceAllString(row.Find(`[itemprop="price"]`).First().Text(), "")
		price, parseErr := strconv.ParseInt(priceText, 10, 64)
		if parseErr != nil || concentration == domain.ConcentrationUnknown || volume == 0 || kind == domain.ProductKindUnknown || price <= 0 || !row.HasClass("tr_avl") {
			return
		}
		variant := *base
		query := variant.Query()
		query.Set("offer", strings.TrimPrefix(row.AttrOr("id", ""), "tr_id_"))
		variant.RawQuery = query.Encode()
		offers = append(offers, domain.Offer{ShopID: "duhirf", RawTitle: title, Brand: product.Brand, Name: product.Name, Edition: product.Edition, Concentration: concentration, VolumeMicroliters: volume, Kind: kind, PriceKopecks: price * 100, InStock: true, URL: variant.String(), RetrievedAt: now})
	})
	if len(offers) == 0 {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("Duhi product offers not found"))
	}
	return offers, nil
}
