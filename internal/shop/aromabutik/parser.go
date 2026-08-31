package aromabutik

import (
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

var yearPattern = regexp.MustCompile(`\s*\(?(19\d{2}|20\d{2})\)?\s*$`)
var digitsPattern = regexp.MustCompile(`\D`)

type productLink struct{ Brand, Name, Edition, URL string }

func parseSearch(reader io.Reader, root string) ([]productLink, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, err)
	}
	return parseSearchDocument(doc, root)
}

func parseSearchDocument(doc *goquery.Document, root string) ([]productLink, error) {
	base, err := url.Parse(root)
	if err != nil {
		return nil, err
	}
	var products []productLink
	doc.Find(".ex_product_item").Each(func(_ int, card *goquery.Selection) {
		link := card.Find(".ex_product_name a").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		brand := strings.TrimSpace(link.Find(".brand").Text())
		clone := link.Clone()
		clone.Find(".brand").Remove()
		rawName := strings.TrimSpace(clone.Text())
		if strings.Contains(domain.NormalizeText(rawName), "по мотивам") {
			return
		}
		match := yearPattern.FindStringSubmatch(rawName)
		edition := ""
		name := rawName
		if len(match) == 2 {
			edition = match[1]
			name = strings.TrimSpace(yearPattern.ReplaceAllString(rawName, ""))
		}
		resolved, resolveErr := base.Parse(href)
		if resolveErr == nil && brand != "" && name != "" {
			products = append(products, productLink{brand, name, edition, resolved.String()})
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
	var offers []domain.Offer
	doc.Find(`tr[itemprop="offers"]`).Each(func(_ int, row *goquery.Selection) {
		title, _ := row.Find(`meta[itemprop="name"]`).Attr("content")
		priceText, _ := row.Find(`meta[itemprop="price"]`).Attr("content")
		price, parseErr := strconv.ParseInt(priceText, 10, 64)
		if parseErr != nil || price <= 0 {
			return
		}
		concentration := domain.ParseConcentration(title)
		volume := domain.ParseVolumeMicroliters(title)
		kind := domain.ClassifyKind(title)
		if kind == domain.ProductKindUnknown && concentration != domain.ConcentrationUnknown {
			kind = domain.ProductKindRetail
		}
		if concentration == domain.ConcentrationUnknown || volume == 0 || kind == domain.ProductKindUnknown || row.Find("button.ex_add_product").Length() == 0 {
			return
		}
		offers = append(offers, domain.Offer{ShopID: "aromabutik", RawTitle: title, Brand: product.Brand, Name: product.Name, Edition: product.Edition, Concentration: concentration, VolumeMicroliters: volume, Kind: kind, PriceKopecks: price * 100, InStock: true, URL: product.URL, RetrievedAt: now})
	})
	if len(offers) == 0 {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("Aroma Butik offers not found"))
	}
	return offers, nil
}

func parseDisplayedRubles(value string) int64 {
	digits := digitsPattern.ReplaceAllString(value, "")
	amount, _ := strconv.ParseInt(digits, 10, 64)
	return amount
}
