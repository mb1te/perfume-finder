package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io"
	"regexp"
	"strings"
	"time"
)

var urlPattern = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?(?:[a-zа-яё0-9-]+\.)+(?:ru|com|net|org|рф|ee)(?:/[^\s<>"']*)?`)

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
		observed := time.Time{}
		if len(strings.TrimSpace(post.Find("a.post-time").Text())) >= 10 {
			observed, _ = time.Parse("2006-01-02", strings.TrimSpace(post.Find("a.post-time").Text())[:10])
		}
		rawURLs := urlPattern.FindAllString(text, -1)
		content.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if href, ok := a.Attr("href"); ok && strings.HasPrefix(href, "http") {
				rawURLs = append(rawURLs, href)
			}
		})
		whitelist := page == 1 && post.HasClass("firstpost") && strings.Contains(text, "Адреса проверенных магазинов")
		normalized := strings.ToLower(text)
		warning := containsAnyRegistry(normalized, "не отгружает", "принимает деньги", "поддел", "мошенн", "не прислали товар", "не связываться")
		alias := containsAnyRegistry(normalized, "бывший", "новый адрес", "называется", "другие имена", "переехал")
		for _, raw := range rawURLs {
			raw = strings.TrimRight(raw, ".,);]…")
			if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
				raw = "https://" + raw
			}
			display, network, nerr := NormalizeDomain(raw)
			if nerr != nil {
				continue
			}
			kind := EvidenceMention
			if whitelist {
				kind = EvidenceWhitelist
			} else if alias {
				kind = EvidenceAlias
			} else if warning {
				kind = EvidenceWarning
			}
			item := Evidence{NetworkDomain: network, DisplayDomain: display, Kind: kind, Page: page, PostURL: postURL, ObservedAt: observed, Excerpt: truncateRunes(text, 500)}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s|%s", network, kind, page, postURL, item.Excerpt)))
			item.ID = hex.EncodeToString(sum[:])
			if !seen[item.ID] {
				seen[item.ID] = true
				result = append(result, item)
			}
		}
	})
	return result, nil
}
func containsAnyRegistry(value string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(value, n) {
			return true
		}
	}
	return false
}
func truncateRunes(value string, limit int) string {
	r := []rune(value)
	if len(r) > limit {
		return string(r[:limit])
	}
	return value
}
