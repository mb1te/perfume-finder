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

func TestBuildSearchURLIncludesExactRequestFields(t *testing.T) {
	got := buildSearchURL(enrichment.Request{
		Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT,
	})
	want := "https://www.fragrantica.ru/search/?query=Dior+Sauvage+2015+edt"
	if got != want {
		t.Fatalf("search URL = %q, want %q", got, want)
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
	renderer := NewBrowserRenderer(allocatorCtx, nil)
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
