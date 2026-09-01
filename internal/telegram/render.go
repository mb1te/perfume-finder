package telegram

import (
	"fmt"
	"net/url"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"sort"
	"strings"
)

func RenderResult(q domain.SearchQuery, result search.Result) string {
	groups := search.GroupAndSort(q, result.Offers)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s · %s\nБез доставки\n", q.Brand, q.Name, q.Concentration, formatVolume(q.VolumeMicroliters))
	order := []domain.ProductKind{domain.ProductKindRetail, domain.ProductKindTester, domain.ProductKindDecant, domain.ProductKindMiniature, domain.ProductKindSample}
	matched := 0
	for _, kind := range order {
		offers := groups[kind]
		if len(offers) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", kindLabel(kind))
		for _, offer := range offers {
			matched++
			fmt.Fprintf(&b, "%s — %s", offer.ShopID, formatRubles(offer.PriceKopecks))
			if safeURL(offer.URL) {
				fmt.Fprintf(&b, " — %s", offer.URL)
			}
			b.WriteString(" — В наличии")
			if !offer.RetrievedAt.IsZero() {
				fmt.Fprintf(&b, " · Проверено %s", offer.RetrievedAt.Format("15:04"))
			}
			b.WriteByte('\n')
		}
	}
	if matched == 0 {
		b.WriteString("\nТочный вариант не найден.\n")
	}
	if len(result.Failures) > 0 {
		sort.Slice(result.Failures, func(i, j int) bool { return result.Failures[i].ShopID < result.Failures[j].ShopID })
		b.WriteString("\nНе ответили: ")
		for i, f := range result.Failures {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.ShopID)
		}
	}
	return b.String()
}
func kindLabel(k domain.ProductKind) string {
	switch k {
	case domain.ProductKindRetail:
		return "Флакон"
	case domain.ProductKindTester:
		return "Тестер"
	case domain.ProductKindDecant:
		return "Отливант"
	case domain.ProductKindMiniature:
		return "Миниатюра"
	case domain.ProductKindSample:
		return "Пробник"
	}
	return string(k)
}
func formatVolume(v int) string {
	if v%1000 == 0 {
		return fmt.Sprintf("%d мл", v/1000)
	}
	return fmt.Sprintf("%d.%03d мл", v/1000, v%1000)
}
func formatRubles(k int64) string {
	s := fmt.Sprintf("%d", k/100)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s + " ₽"
}
func safeURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
