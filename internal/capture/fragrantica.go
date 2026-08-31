package capture

import (
	"context"
	"errors"
	"fmt"
	"github.com/chromedp/chromedp"
	"os"
	"path/filepath"
	"strings"
)

var ErrAccessChallenge = errors.New("Fragrantica access challenge")

func PageURL(base string, page int) string {
	if page <= 1 {
		return base
	}
	return fmt.Sprintf("%s&p=%d", base, page)
}
func ValidateRenderedPage(title, body string) error {
	value := strings.ToLower(title + " " + body)
	for _, marker := range []string{"captcha", "verify you are human", "attention required", "cloudflare"} {
		if strings.Contains(value, marker) {
			return ErrAccessChallenge
		}
	}
	if !strings.Contains(body, "Сообщения с") {
		return fmt.Errorf("Fragrantica topic marker missing")
	}
	return nil
}
func Capture(ctx context.Context, topic string, pages int, output string) error {
	if err := os.MkdirAll(output, 0o750); err != nil {
		return err
	}
	browser, cancel := chromedp.NewContext(ctx)
	defer cancel()
	for page := 1; page <= pages; page++ {
		var title, body, html string
		err := chromedp.Run(browser, chromedp.Navigate(PageURL(topic, page)), chromedp.WaitVisible("main", chromedp.ByQuery), chromedp.Title(&title), chromedp.Text("body", &body, chromedp.ByQuery), chromedp.OuterHTML("html", &html, chromedp.ByQuery))
		if err != nil {
			return fmt.Errorf("capture page %d: %w", page, err)
		}
		if err := ValidateRenderedPage(title, body); err != nil {
			return fmt.Errorf("capture page %d: %w", page, err)
		}
		target := filepath.Join(output, fmt.Sprintf("page-%02d.html", page))
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, []byte(html), 0o640); err != nil {
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			return err
		}
	}
	return nil
}
