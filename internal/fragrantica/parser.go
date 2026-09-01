package fragrantica

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

var ErrAccessChallenge = errors.New("fragrantica access challenge")

var (
	concentrationAlias = regexp.MustCompile(`(?i)\b(?:eau de parfum|eau de toilette|extrait de parfum|eau de cologne|edp|edt|extrait|cologne|parfum|elixir)\b`)
	backgroundColor    = regexp.MustCompile(`(?i)(?:background(?:-color)?\s*:\s*)(#[0-9a-f]{3,8})`)
	accordWidth        = regexp.MustCompile(`(?i)\bwidth\s*:\s*(\d+(?:\.\d+)?)%`)
)

type Candidate struct {
	URL           string
	Brand         string
	Name          string
	Edition       string
	Concentration domain.Concentration
}

type Product struct {
	SourceURL string
	Title     string
	ImageURL  string
	Accords   []enrichment.Accord
}

func ParseSearch(reader io.Reader, _ *url.URL) ([]Candidate, error) {
	doc, err := parseDocument(reader)
	if err != nil {
		return nil, err
	}

	var candidates []Candidate
	doc.Find(`a[href*="/perfume/"][href$=".html"]`).Each(func(_ int, link *goquery.Selection) {
		productURL := safeProductURL(link.AttrOr("href", ""))
		if productURL == "" {
			return
		}
		brand := brandFromProductURL(productURL)
		if brand == "" {
			return
		}
		title := strings.TrimSpace(link.Text())
		concentration := domain.ParseConcentration(title)
		name := normalizedBaseName(title, brand)
		if name == "" {
			return
		}
		candidates = append(candidates, Candidate{
			URL:           productURL,
			Brand:         brand,
			Name:          name,
			Concentration: concentration,
		})
	})
	return candidates, nil
}

func SelectExact(request enrichment.Request, candidates []Candidate) (Candidate, bool) {
	brand := domain.NormalizeText(request.Brand)
	name := domain.NormalizeText(request.Name)
	edition := domain.NormalizeText(request.Edition)
	if brand == "" || name == "" || request.Concentration == domain.ConcentrationUnknown {
		return Candidate{}, false
	}

	var selected Candidate
	found := false
	for _, candidate := range candidates {
		if candidate.URL == "" || candidate.Concentration == domain.ConcentrationUnknown ||
			domain.NormalizeText(candidate.Brand) != brand ||
			domain.NormalizeText(candidate.Name) != name ||
			domain.NormalizeText(candidate.Edition) != edition ||
			candidate.Concentration != request.Concentration {
			continue
		}
		if found {
			return Candidate{}, false
		}
		selected = candidate
		found = true
	}
	return selected, found
}

func ParseProduct(reader io.Reader, _ *url.URL) (Product, error) {
	doc, err := parseDocument(reader)
	if err != nil {
		return Product{}, err
	}

	product := Product{
		Title: strings.TrimSpace(doc.Find("h1").First().Text()),
	}
	if canonical, ok := doc.Find(`link[rel="canonical"]`).First().Attr("href"); ok {
		product.SourceURL = safeProductURL(canonical)
	}
	if image, ok := doc.Find(`meta[property="og:image"]`).First().Attr("content"); ok {
		product.ImageURL = safeImageURL(image)
	}
	doc.Find(`.accord-box .accord-bar, .accord-box [style*="width"]`).Each(func(_ int, bar *goquery.Selection) {
		name := strings.TrimSpace(bar.Text())
		style := bar.AttrOr("style", "")
		color, width, ok := parseAccordStyle(style)
		if !ok || name == "" {
			return
		}
		product.Accords = append(product.Accords, enrichment.Accord{Name: name, Color: color, Width: width})
	})
	return product, nil
}

func parseDocument(reader io.Reader) (*goquery.Document, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if hasAccessChallenge(payload) {
		return nil, ErrAccessChallenge
	}
	return goquery.NewDocumentFromReader(bytes.NewReader(payload))
}

func hasAccessChallenge(payload []byte) bool {
	content := strings.ToLower(string(payload))
	return strings.Contains(content, "just a moment") ||
		strings.Contains(content, "checking your browser") ||
		strings.Contains(content, "attention required") ||
		strings.Contains(content, "challenge-platform") ||
		strings.Contains(content, "cf-chl-")
}

func normalizedBaseName(title, brand string) string {
	normalized := domain.NormalizeText(title)
	normalizedBrand := domain.NormalizeText(brand)
	if strings.HasSuffix(normalized, " "+normalizedBrand) {
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, " "+normalizedBrand))
	}
	normalized = concentrationAlias.ReplaceAllString(normalized, " ")
	return domain.NormalizeText(normalized)
}

func brandFromProductURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(parts) < 3 || parts[0] != "perfume" {
		return ""
	}
	brand, err := url.PathUnescape(parts[1])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(brand, "-", " "))
}

func safeProductURL(raw string) string {
	root, _ := url.Parse("https://www.fragrantica.ru/")
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return ""
	}
	u = root.ResolveReference(u)
	if u.Scheme != "https" || !isProductHost(u.Hostname()) || u.Port() != "" ||
		!strings.HasPrefix(u.Path, "/perfume/") || !strings.HasSuffix(u.Path, ".html") {
		return ""
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func safeImageURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || strings.ToLower(u.Hostname()) != "fimgs.net" || u.Port() != "" {
		return ""
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func isProductHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "fragrantica.ru" || host == "www.fragrantica.ru"
}

func parseAccordStyle(style string) (string, int, bool) {
	colorMatch := backgroundColor.FindStringSubmatch(style)
	widthMatch := accordWidth.FindStringSubmatch(style)
	if len(colorMatch) != 2 || len(widthMatch) != 2 {
		return "", 0, false
	}
	width, err := strconv.ParseFloat(widthMatch[1], 64)
	if err != nil || width < 0 || width > 100 {
		return "", 0, false
	}
	return strings.ToLower(colorMatch[1]), int(math.Round(width)), true
}
