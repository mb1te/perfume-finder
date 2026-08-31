package registry

import (
	"os"
	"strings"
	"testing"
)

func TestParsePageClassifiesWhitelistAndWarnings(t *testing.T) {
	for _, tc := range []struct {
		file string
		page int
		want map[string]EvidenceKind
	}{{"testdata/page-01.html", 1, map[string]EvidenceKind{"randewoo.ru": EvidenceWhitelist, "allureparfum.ru": EvidenceWhitelist, "orental.ru": EvidenceWhitelist, "xn--d1ai6ai.xn--p1ai": EvidenceWhitelist, "aroma-butik.ru": EvidenceWhitelist}}, {"testdata/page-16.html", 16, map[string]EvidenceKind{"artparfum.ru": EvidenceWarning, "parfumday.ru": EvidenceAlias, "montale-mancera.ru": EvidenceAlias}}} {
		f, err := os.Open(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := ParsePage(tc.page, "https://www.fragrantica.ru/board/viewtopic.php?id=235155", f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for domain, kind := range tc.want {
			if !hasEvidence(evidence, domain, kind) {
				t.Fatalf("%s/%s absent on page %d", domain, kind, tc.page)
			}
		}
	}
}
func hasEvidence(items []Evidence, domain string, kind EvidenceKind) bool {
	for _, item := range items {
		if item.NetworkDomain == domain && item.Kind == kind {
			return true
		}
	}
	return false
}

func TestAliasAndWarningEvidenceAreIndependentAndContextual(t *testing.T) {
	file, err := os.Open("testdata/page-16.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	items, err := ParsePage(16, "https://topic", file)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEvidence(items, "artparfum.ru", EvidenceWarning) || !hasEvidence(items, "artparfum.ru", EvidenceAlias) {
		t.Fatal("artparfum warning/alias pair missing")
	}
	for _, domain := range []string{"parfumday.ru", "montale-mancera.ru"} {
		found := false
		for _, item := range items {
			if item.NetworkDomain == domain && item.Kind == EvidenceAlias && item.RelatedNetworkDomain == "artparfum.ru" {
				found = strings.Contains(strings.ToLower(item.Excerpt), domain)
				break
			}
		}
		if !found {
			t.Fatalf("auditable alias for %s missing", domain)
		}
	}
}

func TestOpeningWhitelistKeepsExplicitAliasPairs(t *testing.T) {
	file, err := os.Open("testdata/page-01.html")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	items, err := ParsePage(1, "https://topic", file)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Kind == EvidenceAlias && ((item.NetworkDomain == "scente.ru" && item.RelatedNetworkDomain == "discenter.ru") || (item.NetworkDomain == "discenter.ru" && item.RelatedNetworkDomain == "scente.ru")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("explicit scente/discenter alias missing from opening whitelist")
	}
}

func TestExtractAliasPairsFromLocalPhrase(t *testing.T) {
	text := "Магазин artparfum.ru имеет другие имена, в частности parfumday.ru + montale-mancera.ru"
	pairs := extractAliasPairs(text, normalizeMentions(extractRawURLs(text), text))
	if len(pairs) != 2 {
		t.Fatalf("pairs=%+v", pairs)
	}
}
