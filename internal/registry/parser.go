package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var urlPattern = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?(?:[a-zа-яё0-9-]+\.)+(?:ru|com|net|org|рф|ee)(?:/[^\s<>"']*)?`)

type mention struct{ display, network, raw string }

func ParsePage(page int, topic string, reader io.Reader) ([]Evidence, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, err
	}
	var result []Evidence
	seen := map[string]bool{}
	doc.Find(".post").Each(func(_ int, post *goquery.Selection) {
		content := post.Find(".entry-content").First()
		text := strings.TrimSpace(content.Text())
		if text == "" {
			return
		}
		postURL, _ := post.Find("a.post-time").Attr("href")
		observed := parsePostDate(post.Find("a.post-time").Text())
		rawURLs := urlPattern.FindAllString(text, -1)
		content.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if href, ok := a.Attr("href"); ok && strings.HasPrefix(href, "http") {
				rawURLs = append(rawURLs, href)
			}
		})
		mentions := normalizeMentions(rawURLs)
		if len(mentions) == 0 {
			return
		}
		whitelist := page == 1 && post.HasClass("firstpost") && strings.Contains(text, "Адреса проверенных магазинов")
		normalized := strings.ToLower(text)
		warning := containsAnyRegistry(normalized, "не отгружает", "принимает деньги", "поддел", "мошенн", "не прислали товар", "не связываться")
		alias := containsAnyRegistry(normalized, "бывший", "новый адрес", "называется", "другие имена", "переехал")
		for _, current := range mentions {
			kind := EvidenceMention
			if whitelist {
				kind = EvidenceWhitelist
			}
			appendEvidence(&result, seen, evidenceFor(current, mention{}, kind, page, postURL, observed, text))
			if !whitelist && warning {
				appendEvidence(&result, seen, evidenceFor(current, mention{}, EvidenceWarning, page, postURL, observed, text))
			}
		}
		if !whitelist && alias && len(mentions) > 1 {
			canonical := mentions[0]
			appendEvidence(&result, seen, evidenceFor(canonical, mentions[1], EvidenceAlias, page, postURL, observed, text))
			for _, alternate := range mentions[1:] {
				appendEvidence(&result, seen, evidenceFor(alternate, canonical, EvidenceAlias, page, postURL, observed, text))
			}
		}
	})
	return result, nil
}

func normalizeMentions(rawURLs []string) []mention {
	var result []mention
	seen := map[string]bool{}
	for _, raw := range rawURLs {
		raw = strings.TrimRight(raw, ".,);]…")
		if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
			raw = "https://" + raw
		}
		display, network, err := NormalizeDomain(raw)
		if err == nil && !seen[network] {
			seen[network] = true
			result = append(result, mention{display, network, raw})
		}
	}
	return result
}
func evidenceFor(current, related mention, kind EvidenceKind, page int, postURL string, observed time.Time, text string) Evidence {
	item := Evidence{NetworkDomain: current.network, DisplayDomain: current.display, RelatedNetworkDomain: related.network, RelatedDisplayDomain: related.display, Kind: kind, Page: page, PostURL: postURL, ObservedAt: observed, Excerpt: contextAround(text, current.display, current.network)}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%s|%s", item.NetworkDomain, item.RelatedNetworkDomain, item.Kind, page, postURL, item.Excerpt)))
	item.ID = hex.EncodeToString(sum[:])
	return item
}
func appendEvidence(result *[]Evidence, seen map[string]bool, item Evidence) {
	if !seen[item.ID] {
		seen[item.ID] = true
		*result = append(*result, item)
	}
}
func parsePostDate(value string) time.Time {
	value = strings.TrimSpace(value)
	if len(value) < 10 {
		return time.Time{}
	}
	parsed, _ := time.Parse("2006-01-02", value[:10])
	return parsed
}
func containsAnyRegistry(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
func contextAround(text string, needles ...string) string {
	lower := strings.ToLower(text)
	index := -1
	for _, needle := range needles {
		if needle != "" {
			index = strings.Index(lower, strings.ToLower(needle))
			if index >= 0 {
				break
			}
		}
	}
	runes := []rune(text)
	if index < 0 {
		return truncateRunes(text, 500)
	}
	runeIndex := len([]rune(text[:index]))
	start, end := runeIndex-250, runeIndex+250
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
