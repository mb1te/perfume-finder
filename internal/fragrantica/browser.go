package fragrantica

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"parfumes_finder/internal/enrichment"
)

var ErrBusy = errors.New("Fragrantica renderer busy")

type enrichFunc func(context.Context, enrichment.Request) (enrichment.Card, bool, error)

type enrichResult struct {
	card enrichment.Card
	ok   bool
	err  error
}

type BrowserRenderer struct {
	timeout time.Duration
	slot    chan struct{}
	enrich  enrichFunc
	ready   func(context.Context) error
}

func newBrowserRenderer(timeout time.Duration, enrich enrichFunc) *BrowserRenderer {
	return &BrowserRenderer{
		timeout: timeout,
		slot:    make(chan struct{}, 1),
		enrich:  enrich,
		ready:   func(context.Context) error { return nil },
	}
}

func NewBrowserRenderer(allocatorCtx context.Context, resolver Resolver) *BrowserRenderer {
	guard := NewNetworkGuard(resolver)
	renderer := newBrowserRenderer(15*time.Second, func(ctx context.Context, request enrichment.Request) (enrichment.Card, bool, error) {
		return enrichWithBrowser(ctx, allocatorCtx, guard, request)
	})
	renderer.ready = func(ctx context.Context) error {
		return browserReady(ctx, allocatorCtx)
	}
	return renderer
}

func (renderer *BrowserRenderer) Enrich(ctx context.Context, request enrichment.Request) (enrichment.Card, bool, error) {
	select {
	case renderer.slot <- struct{}{}:
	default:
		return enrichment.Card{}, false, ErrBusy
	}
	lookupCtx, cancel := context.WithTimeout(ctx, renderer.timeout)
	result := make(chan enrichResult, 1)
	go func() {
		card, ok, err := renderer.enrich(lookupCtx, request)
		<-renderer.slot
		result <- enrichResult{card: card, ok: ok, err: err}
	}()
	select {
	case got := <-result:
		cancel()
		return got.card, got.ok, got.err
	case <-lookupCtx.Done():
		cancel()
		return enrichment.Card{}, false, lookupCtx.Err()
	}
}

func (renderer *BrowserRenderer) Ready(ctx context.Context) error {
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return renderer.ready(readyCtx)
}

func browserReady(ctx, allocatorCtx context.Context) error {
	tabCtx, cancel := browserTabContext(ctx, allocatorCtx)
	defer cancel()
	if err := chromedp.Run(tabCtx, chromedp.Navigate("about:blank")); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func enrichWithBrowser(ctx, allocatorCtx context.Context, baseGuard NetworkGuard, request enrichment.Request) (enrichment.Card, bool, error) {
	guard := newLookupGuard(baseGuard)
	searchURL := buildSearchURL(request)
	if err := allowRawURL(ctx, guard, searchURL); err != nil {
		return enrichment.Card{}, false, err
	}

	tabCtx, cancel := browserTabContext(ctx, allocatorCtx)
	defer cancel()
	interceptor := newRequestInterceptor(tabCtx, guard)
	chromedp.ListenTarget(tabCtx, interceptor.handle)
	if err := chromedp.Run(tabCtx, fetch.Enable()); err != nil {
		return enrichment.Card{}, false, browserContextError(ctx, err)
	}

	searchHTML, err := navigateAndCaptureHTML(tabCtx, guard, interceptor, searchURL)
	if err != nil {
		return enrichment.Card{}, false, browserContextError(ctx, err)
	}
	searchBase, _ := url.Parse(searchURL)
	candidates, err := ParseSearch(strings.NewReader(searchHTML), searchBase)
	if err != nil {
		return enrichment.Card{}, false, err
	}
	candidate, ok := SelectExact(request, candidates)
	if !ok {
		return enrichment.Card{}, false, nil
	}
	if err := allowRawURL(ctx, guard, candidate.URL); err != nil {
		return enrichment.Card{}, false, err
	}

	productHTML, err := navigateAndCaptureHTML(tabCtx, guard, interceptor, candidate.URL)
	if err != nil {
		return enrichment.Card{}, false, browserContextError(ctx, err)
	}
	productBase, _ := url.Parse(candidate.URL)
	product, err := ParseProduct(strings.NewReader(productHTML), productBase)
	if err != nil {
		return enrichment.Card{}, false, err
	}
	if product.SourceURL == "" || product.ImageURL == "" {
		return enrichment.Card{}, false, nil
	}
	if err := allowRawURL(ctx, guard, product.SourceURL); err != nil {
		return enrichment.Card{}, false, err
	}
	if err := allowRawURL(ctx, guard, product.ImageURL); err != nil {
		return enrichment.Card{}, false, err
	}

	cardHTML := renderCardHTML(product)
	var png []byte
	if err := chromedp.Run(tabCtx,
		setDocumentContent(cardHTML),
		chromedp.WaitVisible("#perfume-finder-card", chromedp.ByQuery),
		chromedp.Evaluate(`new Promise((resolve, reject) => {
			const image = document.querySelector("#perfume-finder-card img");
			if (!image) { reject(new Error("card image missing")); return; }
			if (image.complete) {
				if (image.naturalWidth > 0) resolve(true); else reject(new Error("card image failed"));
				return;
			}
			image.addEventListener("load", () => resolve(true), {once: true});
			image.addEventListener("error", () => reject(new Error("card image failed")), {once: true});
		})`, nil, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}),
		chromedp.Screenshot("#perfume-finder-card", &png, chromedp.ByQuery),
	); err != nil {
		return enrichment.Card{}, false, browserContextError(ctx, err)
	}
	if len(png) > maxPNGBytes {
		return enrichment.Card{}, false, enrichment.ErrImageTooLarge
	}
	return enrichment.Card{
		SourceURL:   product.SourceURL,
		Title:       product.Title,
		Accords:     product.Accords,
		PNG:         png,
		RetrievedAt: time.Now().UTC(),
	}, true, nil
}

func browserTabContext(ctx, allocatorCtx context.Context) (context.Context, context.CancelFunc) {
	tabCtx, tabCancel := chromedp.NewContext(allocatorCtx)
	stop := context.AfterFunc(ctx, tabCancel)
	return tabCtx, func() {
		stop()
		tabCancel()
	}
}

func browserContextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func buildSearchURL(request enrichment.Request) string {
	query := strings.Join([]string{request.Brand, request.Name, request.Edition, string(request.Concentration)}, " ")
	return "https://www.fragrantica.ru/search/?query=" + url.QueryEscape(query)
}

func allowRawURL(ctx context.Context, guard *lookupGuard, raw string) error {
	target, err := url.Parse(raw)
	if err != nil {
		return ErrUnsafeURL
	}
	return guard.Allow(ctx, target)
}

func navigateAndCaptureHTML(ctx context.Context, guard *lookupGuard, interceptor *requestInterceptor, raw string) (string, error) {
	if err := allowRawURL(ctx, guard, raw); err != nil {
		return "", err
	}
	var markup, location string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(raw),
		chromedp.Location(&location),
		chromedp.OuterHTML("html", &markup, chromedp.ByQuery),
	); err != nil {
		if unsafeErr := interceptor.navigationError(); unsafeErr != nil {
			return "", unsafeErr
		}
		return "", err
	}
	if err := allowRawURL(ctx, guard, location); err != nil {
		return "", err
	}
	return markup, nil
}

type requestInterceptor struct {
	ctx               context.Context
	guard             *lookupGuard
	navigationFailure chan error
	frameMu           sync.Mutex
	mainFrame         cdp.FrameID
}

func newRequestInterceptor(ctx context.Context, guard *lookupGuard) *requestInterceptor {
	return &requestInterceptor{ctx: ctx, guard: guard, navigationFailure: make(chan error, 1)}
}

func (interceptor *requestInterceptor) handle(event any) {
	paused, ok := event.(*fetch.EventRequestPaused)
	if !ok {
		return
	}
	go interceptor.continueOrFail(paused)
}

func (interceptor *requestInterceptor) continueOrFail(paused *fetch.EventRequestPaused) {
	mainDocument := interceptor.isMainDocument(paused)
	target, parseErr := url.Parse(paused.Request.URL)
	err := parseErr
	if err == nil {
		err = interceptor.guard.Allow(interceptor.ctx, target)
	}
	if err != nil {
		if mainDocument {
			select {
			case interceptor.navigationFailure <- ErrUnsafeURL:
			default:
			}
		}
		_ = chromedp.Run(interceptor.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
		}))
		return
	}
	_ = chromedp.Run(interceptor.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return fetch.ContinueRequest(paused.RequestID).Do(ctx)
	}))
}

func (interceptor *requestInterceptor) isMainDocument(paused *fetch.EventRequestPaused) bool {
	if paused.ResourceType != network.ResourceTypeDocument {
		return false
	}
	interceptor.frameMu.Lock()
	defer interceptor.frameMu.Unlock()
	if interceptor.mainFrame == "" {
		interceptor.mainFrame = paused.FrameID
	}
	return paused.FrameID == interceptor.mainFrame
}

func (interceptor *requestInterceptor) navigationError() error {
	select {
	case err := <-interceptor.navigationFailure:
		return err
	default:
		return nil
	}
}

func setDocumentContent(markup string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		frameTree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		return page.SetDocumentContent(frameTree.Frame.ID, markup).Do(ctx)
	})
}

func renderCardHTML(product Product) string {
	var accords bytes.Buffer
	for _, accord := range product.Accords {
		width := max(0, min(accord.Width, 100))
		color := accord.Color
		if _, _, ok := parseAccordStyle("background:" + color + ";width:100%"); !ok {
			color = "#d8c69a"
		}
		fmt.Fprintf(&accords, `<div class="accord"><div class="accord-name">%s</div><div class="accord-track"><div class="accord-fill" style="width:%d%%;background:%s"></div></div></div>`, html.EscapeString(accord.Name), width, color)
	}
	return `<!doctype html><html><head><meta charset="utf-8"><style>
		html,body{margin:0;background:transparent;font-family:Arial,sans-serif}
		#perfume-finder-card{box-sizing:border-box;width:720px;min-height:440px;padding:32px;display:grid;grid-template-columns:240px 1fr;gap:32px;background:#fff;color:#171717;border-radius:24px}
		.bottle{display:flex;align-items:center;justify-content:center}.bottle img{display:block;max-width:220px;max-height:340px;object-fit:contain}
		h1{font-size:30px;line-height:1.15;margin:0 0 24px}.accord{margin:0 0 13px}.accord-name{font-size:16px;margin:0 0 5px}.accord-track{height:17px;background:#eee;border-radius:9px;overflow:hidden}.accord-fill{height:100%;border-radius:9px}
		</style></head><body><article id="perfume-finder-card"><div class="bottle"><img src="` + html.EscapeString(product.ImageURL) + `" alt=""></div><section><h1>` + html.EscapeString(product.Title) + `</h1>` + accords.String() + `</section></article></body></html>`
}
