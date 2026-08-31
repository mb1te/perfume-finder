package registry

import "testing"

func TestNormalizeDomain(t *testing.T) {
	tests := []struct{ input, display, network string }{{"https://www.randewoo.ru/path", "randewoo.ru", "randewoo.ru"}, {"https://духи.рф/catalog", "духи.рф", "xn--d1ai6ai.xn--p1ai"}, {"http://SCENTE.RU/", "scente.ru", "scente.ru"}}
	for _, tt := range tests {
		display, network, err := NormalizeDomain(tt.input)
		if err != nil || display != tt.display || network != tt.network {
			t.Fatalf("NormalizeDomain(%q)=%q,%q,%v", tt.input, display, network, err)
		}
	}
}
func TestNormalizeDomainRejectsUnsafeAndInternalURLs(t *testing.T) {
	for _, input := range []string{"javascript:alert(1)", "https://www.fragrantica.ru/board/", "http://127.0.0.1/", "not a url"} {
		if _, _, err := NormalizeDomain(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
