package fragrantica

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
)

func TestBrowserRendererRejectsSecondConcurrentLookup(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	renderer := newBrowserRenderer(15*time.Second, func(ctx context.Context, _ enrichment.Request) (enrichment.Card, bool, error) {
		close(started)
		select {
		case <-release:
			return enrichment.Card{}, false, nil
		case <-ctx.Done():
			return enrichment.Card{}, false, ctx.Err()
		}
	})
	first := make(chan error, 1)
	go func() { _, _, err := renderer.Enrich(context.Background(), browserRequest); first <- err }()
	<-started
	if _, _, err := renderer.Enrich(context.Background(), browserRequest); !errors.Is(err, ErrBusy) {
		t.Fatalf("second error = %v, want ErrBusy", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestBrowserRendererEnforcesHardTimeoutAndKeepsSlotUntilCleanup(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce sync.Once
	renderer := newBrowserRenderer(20*time.Millisecond, func(context.Context, enrichment.Request) (enrichment.Card, bool, error) {
		startedOnce.Do(func() { close(started) })
		<-release
		return enrichment.Card{}, false, nil
	})
	begin := time.Now()
	_, _, err := renderer.Enrich(context.Background(), browserRequest)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline", err)
	}
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("timeout took %v", elapsed)
	}
	<-started
	if _, _, err := renderer.Enrich(context.Background(), browserRequest); !errors.Is(err, ErrBusy) {
		t.Fatalf("error while timed-out worker cleans up = %v, want ErrBusy", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := renderer.Enrich(context.Background(), browserRequest); !errors.Is(err, ErrBusy) {
			if err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("worker slot was not released after cleanup")
}

func TestBrowserRendererReusesOneRootBrowserAcrossChildTabs(t *testing.T) {
	type contextKey string
	const (
		rootKey contextKey = "root"
		tabKey  contextKey = "tab"
	)
	rootToken := &struct{}{}
	rootStarts, rootStops, tabStarts, tabStops := 0, 0, 0, 0
	badRootParent := false
	runtime := browserRuntime{
		startRoot: func(parent context.Context) (context.Context, context.CancelFunc, error) {
			rootStarts++
			rootCtx, cancel := context.WithCancel(context.WithValue(parent, rootKey, rootToken))
			return rootCtx, func() {
				rootStops++
				cancel()
			}, nil
		},
		newTab: func(_ context.Context, rootCtx context.Context) (context.Context, context.CancelFunc) {
			if rootCtx.Value(rootKey) != rootToken {
				badRootParent = true
			}
			tabStarts++
			tabCtx, cancel := context.WithCancel(context.WithValue(rootCtx, tabKey, tabStarts))
			var once sync.Once
			return tabCtx, func() {
				once.Do(func() {
					tabStops++
					cancel()
				})
			}
		},
		enrich: func(_ context.Context, tabCtx context.Context, _ NetworkGuard, _ enrichment.Request) (enrichment.Card, bool, error) {
			if tabCtx.Value(tabKey) == nil {
				return enrichment.Card{}, false, errors.New("enrichment did not receive a child tab")
			}
			return enrichment.Card{}, false, nil
		},
		ready: func(_ context.Context, tabCtx context.Context) error {
			if tabCtx.Value(tabKey) == nil {
				return errors.New("readiness did not receive a child tab")
			}
			return nil
		},
	}
	renderer, err := newBrowserRendererWithRuntime(context.Background(), nil, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := renderer.Enrich(context.Background(), browserRequest); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := renderer.Enrich(context.Background(), browserRequest); err != nil {
		t.Fatal(err)
	}
	if rootStarts != 1 || rootStops != 0 {
		t.Fatalf("root lifecycle before Close = starts:%d stops:%d, want 1:0", rootStarts, rootStops)
	}
	if badRootParent {
		t.Fatal("a child tab was not created from the allocated root browser context")
	}
	if tabStarts != 3 || tabStops != 3 {
		t.Fatalf("child tab lifecycle = starts:%d stops:%d, want 3:3", tabStarts, tabStops)
	}
	renderer.Close()
	renderer.Close()
	if rootStops != 1 {
		t.Fatalf("root stops after idempotent Close = %d, want 1", rootStops)
	}
}

func TestBuildSearchURLIncludesExactRequestFields(t *testing.T) {
	got := buildSearchURL(enrichment.Request{
		Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT,
	})
	want := "https://www.fragrantica.ru/search/?query=Dior+Sauvage+2015+edt"
	if got != want {
		t.Fatalf("search URL = %q, want %q", got, want)
	}
}

func TestParseSelectedProductRejectsRedirectToDifferentProduct(t *testing.T) {
	markup, err := os.ReadFile("testdata/product-sauvage-edp.html")
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		URL:           "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Brand:         "Dior",
		Name:          "sauvage",
		Concentration: domain.ConcentrationEDP,
	}
	redirected := "https://www.fragrantica.ru/perfume/Dior/Sauvage-2015-Eau-de-Toilette-31861.html"

	if product, ok, err := parseSelectedProduct(browserRequest, candidate, redirected, string(markup)); err != nil || ok {
		t.Fatalf("redirected product = %+v, ok=%v, err=%v", product, ok, err)
	}
}

func TestParseSelectedProductRejectsCanonicalForDifferentProduct(t *testing.T) {
	markup, err := os.ReadFile("testdata/product-sauvage-edp.html")
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		URL:           "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Brand:         "Dior",
		Name:          "sauvage",
		Concentration: domain.ConcentrationEDP,
	}
	markup = []byte(strings.ReplaceAll(string(markup), "Sauvage-Eau-de-Parfum-48100.html", "Sauvage-2015-Eau-de-Toilette-31861.html"))

	if product, ok, err := parseSelectedProduct(browserRequest, candidate, candidate.URL, string(markup)); err != nil || ok {
		t.Fatalf("mismatched canonical product = %+v, ok=%v, err=%v", product, ok, err)
	}
}

func TestParseSelectedProductAcceptsMatchingCandidateFinalAndCanonicalProduct(t *testing.T) {
	markup, err := os.ReadFile("testdata/product-sauvage-edp.html")
	if err != nil {
		t.Fatal(err)
	}
	candidate := Candidate{
		URL:           "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Brand:         "Dior",
		Name:          "sauvage",
		Concentration: domain.ConcentrationEDP,
	}

	product, ok, err := parseSelectedProduct(browserRequest, candidate, candidate.URL, string(markup))
	if err != nil || !ok || product.SourceURL != candidate.URL {
		t.Fatalf("matching product = %+v, ok=%v, err=%v", product, ok, err)
	}
}

func TestRenderCardHTMLEscapesProductMetadata(t *testing.T) {
	product := Product{
		Title:    `<script>alert("title")</script>`,
		ImageURL: `https://fimgs.net/image.jpg?x=1&y=2`,
		Accords: []enrichment.Accord{{
			Name: `<img src=x onerror=alert("accord")>`, Color: `#d8c69a`, Width: 82,
		}},
	}
	got := renderCardHTML(product)
	if strings.Contains(got, product.Title) || strings.Contains(got, product.Accords[0].Name) {
		t.Fatalf("raw metadata reached HTML: %s", got)
	}
	for _, want := range []string{"id=\"perfume-finder-card\"", "&lt;script&gt;", "&lt;img", "width:82%", "https://fimgs.net/image.jpg?x=1&amp;y=2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("card HTML missing %q: %s", want, got)
		}
	}
}

func TestRenderCardHTMLBoundsExternalMetadataBeforeBuildingCard(t *testing.T) {
	accords := make([]enrichment.Accord, 20)
	for index := range accords {
		accords[index] = enrichment.Accord{
			Name:  strings.Repeat("я", 65) + "ACCORD_TAIL",
			Color: "#d8c69a",
			Width: 100,
		}
	}
	got := renderCardHTML(Product{
		Title:    strings.Repeat("я", 130) + "TITLE_TAIL",
		ImageURL: "https://fimgs.net/image.jpg",
		Accords:  accords,
	})

	if strings.Contains(got, "TITLE_TAIL") || strings.Contains(got, "ACCORD_TAIL") {
		t.Fatalf("unbounded metadata reached render HTML")
	}
	if count := strings.Count(got, `class="accord"`); count != 16 {
		t.Fatalf("rendered accord count = %d, want 16", count)
	}
	if !strings.Contains(got, strings.Repeat("я", 128)) || !strings.Contains(got, strings.Repeat("я", 64)) {
		t.Fatal("UTF-8 metadata was not truncated at the byte-safe boundary")
	}
}

func TestRequestInterceptorTracksOnlyMainFrameNavigations(t *testing.T) {
	interceptor := &requestInterceptor{}
	mainDocument := &fetch.EventRequestPaused{FrameID: cdp.FrameID("main"), ResourceType: network.ResourceTypeDocument}
	iframeDocument := &fetch.EventRequestPaused{FrameID: cdp.FrameID("iframe"), ResourceType: network.ResourceTypeDocument}
	mainImage := &fetch.EventRequestPaused{FrameID: cdp.FrameID("main"), ResourceType: network.ResourceTypeImage}
	if !interceptor.isMainDocument(mainDocument) {
		t.Fatal("first document was not recorded as the main frame")
	}
	if interceptor.isMainDocument(iframeDocument) {
		t.Fatal("iframe document was classified as a main-frame navigation")
	}
	if interceptor.isMainDocument(mainImage) {
		t.Fatal("main-frame image was classified as a navigation")
	}
	if !interceptor.isMainDocument(mainDocument) {
		t.Fatal("main-frame redirect was not classified as a navigation")
	}
}

func TestBrowserRendererLive(t *testing.T) {
	if os.Getenv("FRAGRANTICA_LIVE") != "1" {
		t.Skip("set FRAGRANTICA_LIVE=1 to run the live Chromium check")
	}
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	defer allocatorCancel()
	renderer, err := NewBrowserRenderer(allocatorCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer renderer.Close()
	card, ok, err := renderer.Enrich(context.Background(), browserRequest)
	if errors.Is(err, ErrAccessChallenge) {
		t.Skipf("Fragrantica access challenge detected: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Skip("the live site returned no unique exact match")
	}
	if len(card.PNG) == 0 || card.SourceURL == "" {
		t.Fatalf("incomplete live card: %+v", card)
	}
}
