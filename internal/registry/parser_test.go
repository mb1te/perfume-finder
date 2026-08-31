package registry

import (
	"os"
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
