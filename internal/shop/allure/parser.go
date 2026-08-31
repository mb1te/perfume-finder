package allure

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

var (
	trailingYearPattern = regexp.MustCompile(`\s*\(?(19\d{2}|20\d{2})\)?\s*$`)
	nonDigitPattern     = regexp.MustCompile(`\D`)
)

type productLink struct {
	Brand   string
	Name    string
	Edition string
	URL     string
}

func parseSearch(reader io.Reader, baseURL string) ([]productLink, error) {
	document, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("parse Allure search HTML: %w", err))
	}
	return parseSearchDocument(document, baseURL)
}

func parseSearchDocument(document *goquery.Document, baseURL string) ([]productLink, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("parse Allure base URL: %w", err))
	}

	var products []productLink
	document.Find(".catalog-grid__item").Each(func(_ int, card *goquery.Selection) {
		link := card.Find("a.product-card__link").First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		resolved, resolveErr := base.Parse(href)
		if resolveErr != nil || resolved.Scheme != "https" && resolved.Scheme != "http" {
			return
		}
		name, edition := splitEdition(strings.TrimSpace(card.Find(".product-card__name").First().Text()))
		brand := strings.TrimSpace(card.Find(".product-card__brand").First().Text())
		if brand == "" || name == "" {
			return
		}
		products = append(products, productLink{Brand: brand, Name: name, Edition: edition, URL: resolved.String()})
	})
	return products, nil
}

func parseProduct(reader io.Reader, product productLink, retrievedAt time.Time) ([]domain.Offer, error) {
	document, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("parse Allure product HTML: %w", err))
	}
	return parseProductDocument(document, product, retrievedAt)
}

func parseProductDocument(document *goquery.Document, product productLink, retrievedAt time.Time) ([]domain.Offer, error) {
	productURL, err := url.Parse(product.URL)
	if err != nil {
		return nil, shop.NewError(shop.ErrorParse, fmt.Errorf("parse Allure product URL: %w", err))
	}

	var offers []domain.Offer
	document.Find(".offer-item").Each(func(_ int, item *goquery.Selection) {
		titleNode := item.Find(".offer-title__name").First().Clone()
		titleNode.Children().Remove()
		title := strings.TrimSpace(titleNode.Text())
		kind := classifyOfferKind(title)
		if kind == domain.ProductKindUnknown {
			return
		}
		concentration := domain.ParseConcentration(title)
		volume := domain.ParseVolumeMicroliters(item.Find(".offer-item__volume-availability .offer-volume").First().Text())
		price := parseRubles(item.Find(".offer-price--inner").First().Text())
		if concentration == domain.ConcentrationUnknown || volume == 0 {
			return
		}

		availability := domain.NormalizeText(item.Find(".offer-item__volume-availability .offer-availability").First().Text())
		if !strings.Contains(availability, "в наличии") || price <= 0 {
			return
		}
		variantURL := *productURL
		variantQuery := variantURL.Query()
		buyHref, ok := item.Find("a.button-buy").First().Attr("href")
		if !ok {
			return
		}
		buyURL, parseErr := url.Parse(buyHref)
		if parseErr != nil || buyURL.Query().Get("id") == "" {
			return
		}
		variantQuery.Set("el", buyURL.Query().Get("id"))
		variantURL.RawQuery = variantQuery.Encode()

		offers = append(offers, domain.Offer{
			ShopID:            "allure",
			RawTitle:          strings.TrimSpace(product.Brand + " " + product.Name + " " + product.Edition + " " + title),
			Brand:             product.Brand,
			Name:              product.Name,
			Edition:           product.Edition,
			Concentration:     concentration,
			VolumeMicroliters: volume,
			Kind:              kind,
			PriceKopecks:      price * 100,
			InStock:           true,
			URL:               variantURL.String(),
			RetrievedAt:       retrievedAt,
		})
	})
	return offers, nil
}

func splitEdition(value string) (string, string) {
	match := trailingYearPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return strings.TrimSpace(value), ""
	}
	name := strings.TrimSpace(trailingYearPattern.ReplaceAllString(value, ""))
	return name, match[1]
}

func classifyOfferKind(title string) domain.ProductKind {
	if kind := domain.ClassifyKind(title); kind != domain.ProductKindUnknown {
		return kind
	}
	if !strings.Contains(title, "(") {
		return domain.ProductKindRetail
	}
	return domain.ProductKindUnknown
}

func parseRubles(value string) int64 {
	digits := nonDigitPattern.ReplaceAllString(value, "")
	if digits == "" {
		return 0
	}
	amount, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0
	}
	return amount
}
