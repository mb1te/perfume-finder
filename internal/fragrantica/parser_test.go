package fragrantica

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

func TestParseSearchSeparatesSauvageVariants(t *testing.T) {
	candidates := parseSearchFixture(t, "search-sauvage.html")
	if len(candidates) != 4 {
		t.Fatalf("candidates = %+v", candidates)
	}

	want := []struct {
		name          string
		concentration domain.Concentration
	}{
		{name: "sauvage", concentration: domain.ConcentrationEDP},
		{name: "sauvage", concentration: domain.ConcentrationEDT},
		{name: "eau sauvage", concentration: domain.ConcentrationUnknown},
		{name: "sauvage into the wild", concentration: domain.ConcentrationEDT},
	}
	for i, expected := range want {
		if candidates[i].Brand != "Dior" || candidates[i].Name != expected.name || candidates[i].Concentration != expected.concentration {
			t.Fatalf("candidate %d = %+v, want brand Dior name %q concentration %q", i, candidates[i], expected.name, expected.concentration)
		}
	}
}

func TestSelectExactRejectsFlankersAndConcentrationMismatch(t *testing.T) {
	candidates := parseSearchFixture(t, "search-sauvage.html")
	got, ok := SelectExact(enrichment.Request{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
	}, candidates)
	if !ok || !strings.Contains(got.URL, "48100") {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestSelectExactMatchesEditionExplicitInCardSlug(t *testing.T) {
	candidates := parseSearchFixture(t, "search-sauvage.html")
	got, ok := SelectExact(enrichment.Request{
		Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT,
	}, candidates)
	if !ok || !strings.Contains(got.URL, "31861") || got.Edition != "2015" {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestSelectExactRemovesRussianConcentrationAliasFromBaseName(t *testing.T) {
	candidates := parseSearchFixture(t, "search-sauvage-ru.html")
	got, ok := SelectExact(enrichment.Request{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
	}, candidates)
	if !ok || got.Name != "sauvage" || got.Concentration != domain.ConcentrationEDP {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestSelectExactRequiresExactNormalizedBrandNameEditionAndConcentration(t *testing.T) {
	request := enrichment.Request{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP}
	matching := Candidate{URL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html", Brand: "DIOR", Name: " SAUVAGE ", Edition: "2015", Concentration: domain.ConcentrationEDP}
	if got, ok := SelectExact(request, []Candidate{matching}); !ok || got.URL != matching.URL {
		t.Fatalf("matching candidate rejected: %+v %v", got, ok)
	}

	for _, candidate := range []Candidate{
		{URL: matching.URL, Brand: "Dior Homme", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP},
		{URL: matching.URL, Brand: "Dior", Name: "Sauvage Elixir", Edition: "2015", Concentration: domain.ConcentrationEDP},
		{URL: matching.URL, Brand: "Dior", Name: "Sauvage", Edition: "", Concentration: domain.ConcentrationEDP},
		{URL: matching.URL, Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationUnknown},
		{URL: matching.URL, Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT},
	} {
		if _, ok := SelectExact(request, []Candidate{candidate}); ok {
			t.Fatalf("weak candidate accepted: %+v", candidate)
		}
	}
}

func TestSelectExactRejectsAmbiguousExactMatches(t *testing.T) {
	request := enrichment.Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
	candidate := Candidate{URL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html", Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
	if _, ok := SelectExact(request, []Candidate{candidate, candidate}); ok {
		t.Fatal("ambiguity accepted")
	}
}

func TestParseProductExtractsSafeSourceImageAndAccords(t *testing.T) {
	got := parseProductFixture(t, "product-sauvage-edp.html")
	if got.SourceURL != "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html" {
		t.Fatalf("source URL = %q", got.SourceURL)
	}
	if got.Title != "Sauvage Eau de Parfum Dior" || got.ImageURL != "https://fimgs.net/mdimg/perfume/social.48100.jpg" {
		t.Fatalf("product metadata = %+v", got)
	}
	if len(got.Accords) != 2 || got.Accords[0] != (enrichment.Accord{Name: "свежий пряный", Color: "#d8c69a", Width: 100}) || got.Accords[1] != (enrichment.Accord{Name: "цитрусовый", Color: "#f2d35c", Width: 82}) {
		t.Fatalf("accords = %+v", got.Accords)
	}
}

func TestParseProductRejectsUntrustedCanonicalAndImageURLs(t *testing.T) {
	base, err := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html")
	if err != nil {
		t.Fatal(err)
	}
	product, err := ParseProduct(strings.NewReader(`<link rel="canonical" href="https://evil.example/perfume/Dior/Sauvage.html">`), base)
	if err != nil {
		t.Fatal(err)
	}
	if product.SourceURL != "" {
		t.Fatalf("unsafe canonical URL accepted: %+v", product)
	}

	product, err = ParseProduct(strings.NewReader(`<link rel="canonical" href="https://www.fragrantica.ru/perfume/Dior/Sauvage.html"><meta property="og:image" content="https://evil.example/image.jpg">`), base)
	if err != nil {
		t.Fatal(err)
	}
	if product.SourceURL != "https://www.fragrantica.ru/perfume/Dior/Sauvage.html" || product.ImageURL != "" {
		t.Fatalf("unsafe image URL accepted: %+v", product)
	}
}

func TestParseRejectsAccessChallenge(t *testing.T) {
	file, err := os.Open(filepath.Join("testdata", "challenge.html"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	base, err := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSearch(file, base); !errors.Is(err, ErrAccessChallenge) {
		t.Fatalf("search error = %v", err)
	}
	file.Seek(0, 0)
	if _, err := ParseProduct(file, base); !errors.Is(err, ErrAccessChallenge) {
		t.Fatalf("product error = %v", err)
	}
}

func TestParseRejectsCommonAccessChallengeMarkers(t *testing.T) {
	base, err := url.Parse("https://www.fragrantica.ru/search/?query=Dior+Sauvage")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		`<title>CAPTCHA</title>`,
		`<div class="hcaptcha-box"></div>`,
		`<div class="g-recaptcha"></div>`,
		`<h1>Verify You Are Human</h1>`,
	} {
		t.Run(marker, func(t *testing.T) {
			if _, err := ParseSearch(strings.NewReader(marker), base); !errors.Is(err, ErrAccessChallenge) {
				t.Fatalf("marker %q error = %v", marker, err)
			}
		})
	}
}

func parseSearchFixture(t *testing.T, name string) []Candidate {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	base, err := url.Parse("https://www.fragrantica.ru/search/?query=Dior+Sauvage")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := ParseSearch(file, base)
	if err != nil {
		t.Fatal(err)
	}
	return candidates
}

func parseProductFixture(t *testing.T, name string) Product {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	base, err := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html")
	if err != nil {
		t.Fatal(err)
	}
	product, err := ParseProduct(file, base)
	if err != nil {
		t.Fatal(err)
	}
	return product
}
