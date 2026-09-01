# Fragrantica Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show only store-observed perfume concentrations and send a best-effort Fragrantica bottle-and-accord card after the price result.

**Architecture:** Keep the Go price bot in its existing distroless container and add a private single-worker Chromium sidecar. The bot stores enrichment cards in SQLite, sends price text before requesting enrichment, and cancels stale enrichment when a newer Telegram search supersedes it.

**Tech Stack:** Go 1.26.3, SQLite via `modernc.org/sqlite`, `goquery`, `chromedp`, `go-telegram/bot`, Docker Compose, Alpine Chromium.

**Spec:** `docs/superpowers/specs/2026-09-01-fragrantica-enrichment-design.md`

## Global Constraints

- Fragrantica enrichment is optional and must never suppress or delay delivery of price results.
- Never bypass Cloudflare, CAPTCHA, or human-verification challenges.
- Sidecar lookup timeout is 15 seconds and browser concurrency is exactly one.
- Decoded PNG size is at most 5 MB; SQLite enrichment cache is at most 256 MB and prunes oldest entries to 192 MB.
- Exact Fragrantica matching requires normalized brand, base fragrance name, supplied edition, and parsed concentration; ambiguity returns no card.
- Chromium sidecar is private, non-root, read-only, limited to 512 MB RAM and 128 MB shared memory.
- No network, SQLite, browser, or Telegram I/O may run while the global active-search mutex is held.
- New behavior is implemented test-first; default tests do not require live Fragrantica.

---

## File Structure

- `internal/domain/query.go`: shared `FragranceCandidate` and concentration ordering.
- `internal/storage/session.go`: persisted candidates and legacy-session compatibility.
- `internal/telegram/handler.go`: observed-concentration flow, search ownership, and enrichment orchestration.
- `internal/enrichment/model.go`: request/card/error contracts shared by bot and sidecar.
- `internal/enrichment/client.go`: bounded sidecar HTTP client and cache decorator.
- `internal/storage/enrichment.go`: seven-day SQLite card cache and size pruning.
- `internal/storage/migrations/003_fragrantica_enrichment.sql`: cache schema.
- `internal/fragrantica/parser.go`: rendered search/product parsing and exact candidate selection.
- `internal/fragrantica/browser.go`: single Chromium worker, challenge detection, guarded navigation, and card screenshot.
- `internal/fragrantica/server.go`: `/v1/enrich` and `/healthz` HTTP API.
- `cmd/fragranticaenricher/main.go`: sidecar process wiring.
- `internal/telegram/transport.go`: PNG upload through Telegram `sendPhoto`.
- `cmd/perfumebot/main.go`, `internal/config/config.go`: optional enricher wiring.
- `Dockerfile.enricher`, `docker-compose.yml`, `.env.example`, `README.md`: deployment.

---

### Task 1: Observed Concentration Candidates

**Files:**
- Modify: `internal/domain/query.go`
- Modify: `internal/storage/session.go`
- Modify: `internal/storage/storage_test.go`
- Modify: `internal/telegram/handler.go`
- Modify: `internal/telegram/handler_test.go`

**Interfaces:**
- Produces: `domain.FragranceCandidate{Query domain.SearchQuery, Concentrations []domain.Concentration}`.
- Produces: `domain.SortConcentrations([]domain.Concentration) []domain.Concentration` with order EDT, EDP, Parfum, Extrait, Cologne, Elixir.
- Produces: `storage.Session.Candidates []domain.FragranceCandidate` and `storage.Session.Concentrations []domain.Concentration`.
- Preserves: loading legacy session JSON containing `options: []SearchQuery`.

- [ ] **Step 1: Write failing domain, storage, and handler tests**

Add tests proving candidate aggregation deduplicates concentrations, excludes `unknown`, sorts deterministically, auto-selects one, shows only multiple observed values, honors explicit input, and asks for a complete retry when none are observed.

```go
func TestCandidatesCarryOnlyObservedConcentrations(t *testing.T) {
    offers := []domain.Offer{
        {Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP},
        {Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT},
        {Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationUnknown},
        {Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP},
    }
    got := candidatesFromOffers(offers, domain.SearchQuery{})
    want := []domain.Concentration{domain.ConcentrationEDT, domain.ConcentrationEDP}
    if len(got) != 1 || !reflect.DeepEqual(got[0].Concentrations, want) {
        t.Fatalf("candidates = %+v, want concentrations %v", got, want)
    }
}

func TestSingleObservedConcentrationIsSelectedAutomatically(t *testing.T) {
    messenger := &fakeMessenger{}
    sessions := newMemorySessions()
    h := NewHandler(messenger, sessions, fakeSearcher{result: search.Result{Offers: []domain.Offer{{
        Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
    }}}})
    if err := h.HandleMessage(context.Background(), 42, "Dior Sauvage"); err != nil {
        t.Fatal(err)
    }
    if sessions.values[42].Query.Concentration != domain.ConcentrationEDP {
        t.Fatalf("query = %+v", sessions.values[42].Query)
    }
    if strings.Contains(messenger.messages[len(messenger.messages)-1].Text, "Выбери концентрацию") {
        t.Fatal("single concentration prompted the user")
    }
}

func TestMultipleObservedConcentrationsAreTheOnlyButtons(t *testing.T) {
    message := concentrationMessage("session-1", []domain.Concentration{
        domain.ConcentrationElixir, domain.ConcentrationEDP,
    })
    got := make([]string, 0, len(message.Buttons))
    for _, button := range message.Buttons { got = append(got, button.Text) }
    if want := []string{"EDP", "Elixir"}; !reflect.DeepEqual(got, want) {
        t.Fatalf("buttons = %v, want %v", got, want)
    }
}

func TestNoObservedConcentrationRequestsExplicitRetry(t *testing.T) {
    messenger := &fakeMessenger{}
    sessions := newMemorySessions()
    h := NewHandler(messenger, sessions, fakeSearcher{})
    session := storage.Session{ChatID: 42, ID: "session-1", Query: domain.SearchQuery{
        Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationUnknown,
    }}
    sessions.values[42] = session
    if err := h.advance(context.Background(), session); err != nil { t.Fatal(err) }
    got := messenger.messages[len(messenger.messages)-1]
    if !strings.Contains(got.Text, "Dior Sauvage EDP") || len(got.Buttons) != 0 {
        t.Fatalf("message = %+v", got)
    }
    if _, ok := sessions.values[42]; ok { t.Fatal("retry session was not deleted") }
}

func TestExplicitConcentrationWinsWhenDiscoveryHasAnotherValue(t *testing.T) {
    candidates := candidatesFromOffers([]domain.Offer{{
        Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT,
    }}, domain.SearchQuery{Concentration: domain.ConcentrationEDP})
    got := mergeCandidate(domain.SearchQuery{Concentration: domain.ConcentrationEDP}, candidates[0].Query)
    if got.Concentration != domain.ConcentrationEDP { t.Fatalf("concentration = %q", got.Concentration) }
}
```

Update the storage round-trip test to persist `Candidates` and `Concentrations`, and add a legacy JSON row whose `options` field still loads into candidate queries.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/domain ./internal/storage ./internal/telegram -run 'Concentration|Candidate|Sessions' -count=1
```

Expected: compile failures for `FragranceCandidate`, `Session.Candidates`, and the changed candidate result type.

- [ ] **Step 3: Implement the minimal candidate and flow changes**

Add the domain type and canonical order:

```go
type FragranceCandidate struct {
    Query          SearchQuery
    Concentrations []Concentration
}

func SortConcentrations(values []Concentration) []Concentration {
    order := map[Concentration]int{
        ConcentrationEDT: 0, ConcentrationEDP: 1, ConcentrationParfum: 2,
        ConcentrationExtrait: 3, ConcentrationCologne: 4, ConcentrationElixir: 5,
    }
    result := append([]Concentration(nil), values...)
    sort.Slice(result, func(i, j int) bool { return order[result[i]] < order[result[j]] })
    return result
}
```

Persist new candidate fields while accepting legacy options:

```go
type sessionPayload struct {
    ID             string                       `json:"id"`
    Query          domain.SearchQuery           `json:"query"`
    Candidates     []domain.FragranceCandidate  `json:"candidates,omitempty"`
    Concentrations []domain.Concentration       `json:"concentrations,omitempty"`
    LegacyOptions  []domain.SearchQuery          `json:"options,omitempty"`
}
```

Change `candidatesFromOffers` to aggregate a normalized candidate key and a set of recognized concentrations. In `advance`, use the selected candidate's concentrations:

```go
case session.Query.Concentration == domain.ConcentrationUnknown:
    switch len(session.Concentrations) {
    case 0:
        _ = h.sessions.Delete(ctx, session.ChatID)
        return h.messenger.Send(ctx, session.ChatID, Message{
            Text: "Не удалось определить концентрацию. Повтори запрос целиком, например: Dior Sauvage EDP.",
        })
    case 1:
        session.Query.Concentration = session.Concentrations[0]
        return h.advance(ctx, session)
    default:
        session.Stage = "choose_concentration"
        message = concentrationMessage(session.ID, session.Concentrations)
    }
```

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./internal/domain ./internal/storage ./internal/telegram -count=1
```

Expected: PASS; no button exists for a concentration absent from discovery offers.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/storage/session.go internal/storage/storage_test.go internal/telegram/handler.go internal/telegram/handler_test.go
git commit -m "fix: offer only observed concentrations"
```

---

### Task 2: Non-Blocking Search Ownership

**Files:**
- Modify: `internal/telegram/handler.go`
- Modify: `internal/telegram/handler_test.go`

**Interfaces:**
- Produces: `Handler.isOwned(chatID int64, id string) bool`.
- Produces: `Handler.finishOwned(chatID int64, id string)`.
- Requires: all owned I/O use the cancelable context returned by `beginSearch`.

- [ ] **Step 1: Write failing concurrency tests**

Add a messenger that blocks one chat's send while another chat starts and completes. Also retain the existing supersession test and assert a canceled first context cannot send after the second search begins.

```go
func TestSlowSendDoesNotBlockAnotherChat(t *testing.T) {
    blocking := newBlockingMessenger(42)
    h := NewHandler(blocking, newMemorySessions(), fixedSearcher())
    firstDone := make(chan error, 1)
    go func() { firstDone <- h.HandleMessage(context.Background(), 42, "Dior Sauvage EDP 100 мл") }()
    <-blocking.started

    secondDone := make(chan error, 1)
    go func() { secondDone <- h.HandleMessage(context.Background(), 84, "Dior Sauvage EDP 100 мл") }()
    select {
    case err := <-secondDone:
        if err != nil { t.Fatal(err) }
    case <-time.After(200 * time.Millisecond):
        t.Fatal("chat 84 blocked behind chat 42 I/O")
    }
    close(blocking.release)
    if err := <-firstDone; err != nil { t.Fatal(err) }
}

type blockingMessenger struct {
    blockedChat int64
    started chan struct{}
    release chan struct{}
    once sync.Once
}

func newBlockingMessenger(chatID int64) *blockingMessenger {
    return &blockingMessenger{blockedChat: chatID, started: make(chan struct{}), release: make(chan struct{})}
}

func (m *blockingMessenger) Send(ctx context.Context, chatID int64, _ Message) error {
    if chatID != m.blockedChat { return nil }
    m.once.Do(func() { close(m.started) })
    select {
    case <-m.release:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func fixedSearcher() Searcher {
    return fakeSearcher{result: search.Result{Offers: []domain.Offer{{
        Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
        VolumeMicroliters: 100000, Kind: domain.ProductKindRetail,
    }}}}
}
```

- [ ] **Step 2: Run the concurrency tests and verify RED**

Run:

```bash
go test -race ./internal/telegram -run 'SlowSend|Supersed' -count=1
```

Expected: `TestSlowSendDoesNotBlockAnotherChat` times out because `completeOwned` holds `activeMu` during messenger/session I/O.

- [ ] **Step 3: Replace `completeOwned` with short ownership operations**

```go
func (h *Handler) isOwned(chatID int64, id string) bool {
    h.activeMu.Lock()
    defer h.activeMu.Unlock()
    current, ok := h.active[chatID]
    return ok && current.id == id
}

func (h *Handler) finishOwned(chatID int64, id string) {
    h.activeMu.Lock()
    defer h.activeMu.Unlock()
    current, ok := h.active[chatID]
    if !ok || current.id != id { return }
    delete(h.active, chatID)
    current.cancel()
}
```

Perform I/O with `searchCtx`, check `isOwned` immediately before each session mutation/send, and defer `finishOwned`. A newer `beginSearch` cancels that context, so a superseded Telegram send returns without publishing stale output.

- [ ] **Step 4: Run race tests and verify GREEN**

Run:

```bash
go test -race ./internal/telegram -count=1
```

Expected: PASS with cross-chat progress and no stale superseded messages.

- [ ] **Step 5: Commit**

```bash
git add internal/telegram/handler.go internal/telegram/handler_test.go
git commit -m "fix: keep Telegram ownership locks out of I/O"
```

---

### Task 3: Enrichment Contract and SQLite Cache

**Files:**
- Create: `internal/enrichment/model.go`
- Create: `internal/storage/enrichment.go`
- Create: `internal/storage/enrichment_test.go`
- Create: `internal/storage/migrations/003_fragrantica_enrichment.sql`

**Interfaces:**
- Produces: `enrichment.Request`, `enrichment.Accord`, `enrichment.Card`.
- Produces: `enrichment.Service.Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error)`.
- Produces: `storage.EnrichmentCache.Get/Put` with seven-day TTL, 5 MB entry limit, and 256/192 MB pruning.

- [ ] **Step 1: Write failing migration, round-trip, expiry, and pruning tests**

```go
func TestEnrichmentCacheRoundTripAndExpiry(t *testing.T) {
    now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
    cache := NewEnrichmentCache(openTestDB(t), 7*24*time.Hour, func() time.Time { return now })
    req := enrichment.Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
    want := enrichment.Card{
        SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
        Title: "Sauvage Eau de Parfum Dior",
        Accords: []enrichment.Accord{{Name: "свежий пряный", Color: "#d8c69a", Width: 100}},
        PNG: tinyPNG(t),
    }
    if err := cache.Put(context.Background(), req, want); err != nil { t.Fatal(err) }
    got, ok, err := cache.Get(context.Background(), req)
    if err != nil || !ok || !reflect.DeepEqual(got, want) { t.Fatalf("got %+v %v %v", got, ok, err) }
    now = now.Add(7 * 24 * time.Hour)
    if _, ok, err := cache.Get(context.Background(), req); err != nil || ok { t.Fatalf("expired: %v %v", ok, err) }
}

func TestEnrichmentCacheRejectsOversizedPNG(t *testing.T) {
    cache := NewEnrichmentCache(openTestDB(t), 7*24*time.Hour, time.Now)
    request := enrichment.Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
    card := enrichment.Card{PNG: make([]byte, 5<<20+1)}
    if err := cache.Put(context.Background(), request, card); !errors.Is(err, enrichment.ErrImageTooLarge) {
        t.Fatalf("error = %v", err)
    }
}

func tinyPNG(t *testing.T) []byte {
    t.Helper()
    data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
    if err != nil { t.Fatal(err) }
    return data
}
```

Insert enough sub-5 MB rows to cross 256 MB and assert oldest entries are removed until `SUM(length(image_png)) <= 192<<20`.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
go test ./internal/storage -run Enrichment -count=1
```

Expected: compile failure because enrichment types/cache do not exist.

- [ ] **Step 3: Add types, schema, canonical key, TTL, and pruning**

```go
type Request struct {
    Brand, Name, Edition string
    Concentration domain.Concentration
}

type Accord struct { Name, Color string; Width int }
type Card struct {
    SourceURL string
    Title string
    Accords []Accord
    PNG []byte
    RetrievedAt time.Time
}

type Service interface {
    Enrich(context.Context, Request) (Card, bool, error)
}
```

Schema:

```sql
CREATE TABLE IF NOT EXISTS fragrantica_enrichment_cache (
    cache_key TEXT PRIMARY KEY,
    source_url TEXT NOT NULL,
    title TEXT NOT NULL,
    accords_json BLOB NOT NULL,
    image_png BLOB NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_fragrantica_enrichment_created_at
    ON fragrantica_enrichment_cache(created_at);
```

Use SHA-256 over normalized request JSON. In `Put`, reject `len(card.PNG) > 5<<20`, upsert, calculate `SUM(length(image_png))`, and delete oldest rows in one transaction until the size is at or below `192<<20` whenever it crossed `256<<20`.

- [ ] **Step 4: Run storage tests and verify GREEN**

Run:

```bash
go test ./internal/storage -count=1
```

Expected: PASS, including opening a database containing migrations 001–003.

- [ ] **Step 5: Commit**

```bash
git add internal/enrichment/model.go internal/storage/enrichment.go internal/storage/enrichment_test.go internal/storage/migrations/003_fragrantica_enrichment.sql
git commit -m "feat: cache Fragrantica enrichment cards"
```

---

### Task 4: Fragrantica Rendered-Page Parser

**Files:**
- Create: `internal/fragrantica/parser.go`
- Create: `internal/fragrantica/parser_test.go`
- Create: `internal/fragrantica/testdata/search-sauvage.html`
- Create: `internal/fragrantica/testdata/product-sauvage-edp.html`
- Create: `internal/fragrantica/testdata/challenge.html`

**Interfaces:**
- Produces: `ParseSearch(io.Reader, *url.URL) ([]Candidate, error)`.
- Produces: `SelectExact(enrichment.Request, []Candidate) (Candidate, bool)`.
- Produces: `ParseProduct(io.Reader, *url.URL) (Product, error)`.
- Produces: `ErrAccessChallenge` and `Product{SourceURL, Title, ImageURL string, Accords []enrichment.Accord}`.

- [ ] **Step 1: Add minimal sanitized fixtures and failing parser tests**

The search fixture must include an EDP page, EDT page, `Eau Sauvage`, and `Sauvage Into The Wild`. Product fixture must contain a canonical link, `og:image`, and accord bars.

```html
<a href="/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html">
  <img src="https://fimgs.net/mdimg/perfume-thumbs/dark-m.48100.avif">
  Sauvage Eau de Parfum Dior
</a>
<a href="/perfume/Dior/Eau-Sauvage-231.html">Eau Sauvage Dior</a>
```

```html
<link rel="canonical" href="https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html">
<meta property="og:image" content="https://fimgs.net/mdimg/perfume/social.48100.jpg">
<div class="accord-box">
  <div class="accord-bar" style="background:#d8c69a;width:100%">свежий пряный</div>
  <div class="accord-bar" style="background:#f2d35c;width:82%">цитрусовый</div>
</div>
```

```go
func TestSelectExactRejectsFlankersAndConcentrationMismatch(t *testing.T) {
    candidates := parseSearchFixture(t, "search-sauvage.html")
    got, ok := SelectExact(enrichment.Request{
        Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
    }, candidates)
    if !ok || !strings.Contains(got.URL, "48100") { t.Fatalf("got %+v %v", got, ok) }
}

func TestSelectExactRejectsAmbiguousExactMatches(t *testing.T) {
    candidate := Candidate{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
    if _, ok := SelectExact(request, []Candidate{candidate, candidate}); ok { t.Fatal("ambiguity accepted") }
}

func parseSearchFixture(t *testing.T, name string) []Candidate {
    t.Helper()
    file, err := os.Open(filepath.Join("testdata", name))
    if err != nil { t.Fatal(err) }
    defer file.Close()
    base, _ := url.Parse("https://www.fragrantica.ru/search/?query=Dior+Sauvage")
    candidates, err := ParseSearch(file, base)
    if err != nil { t.Fatal(err) }
    return candidates
}

func parseProductFixture(t *testing.T, name string) Product {
    t.Helper()
    file, err := os.Open(filepath.Join("testdata", name))
    if err != nil { t.Fatal(err) }
    defer file.Close()
    base, _ := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html")
    product, err := ParseProduct(file, base)
    if err != nil { t.Fatal(err) }
    return product
}

func TestParseProductExtractsImageAndAccords(t *testing.T) {
    got := parseProductFixture(t, "product-sauvage-edp.html")
    if len(got.Accords) != 2 || got.Accords[0].Width != 100 || !strings.Contains(got.ImageURL, "fimgs.net") {
        t.Fatalf("product = %+v", got)
    }
}
```

- [ ] **Step 2: Run parser tests and verify RED**

Run:

```bash
go test ./internal/fragrantica -count=1
```

Expected: package/types are missing.

- [ ] **Step 3: Implement conservative parsing and matching**

Use `goquery` selectors:

```go
doc.Find(`a[href*="/perfume/"][href$=".html"]`)
doc.Find(`link[rel="canonical"]`).Attr("href")
doc.Find(`meta[property="og:image"]`).Attr("content")
doc.Find(`.accord-box .accord-bar, .accord-box [style*="width"]`)
```

Normalize candidate titles by removing the exact brand suffix and concentration aliases, then require exact normalized brand/base name/edition/concentration. Resolve relative URLs only against `https://www.fragrantica.ru`; accept product hosts `fragrantica.ru` and `www.fragrantica.ru`, and image host `fimgs.net`. Detect challenge markers before parsing content.

- [ ] **Step 4: Run parser tests and verify GREEN**

Run:

```bash
go test ./internal/fragrantica -count=1
```

Expected: PASS for exact, flanker, ambiguity, challenge, and extraction cases.

- [ ] **Step 5: Commit**

```bash
git add internal/fragrantica
git commit -m "feat: parse exact Fragrantica cards"
```

---

### Task 5: Single-Worker Chromium Sidecar API

**Files:**
- Create: `internal/fragrantica/browser.go`
- Create: `internal/fragrantica/browser_test.go`
- Create: `internal/fragrantica/netguard.go`
- Create: `internal/fragrantica/netguard_test.go`
- Create: `internal/fragrantica/server.go`
- Create: `internal/fragrantica/server_test.go`
- Create: `cmd/fragranticaenricher/main.go`

**Interfaces:**
- Produces: `Renderer.Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error)` and `Renderer.Ready(context.Context) error`.
- Produces: `BrowserRenderer` with a capacity-one semaphore and 15-second timeout.
- Produces: `NetworkGuard.Allow(context.Context, *url.URL) error`, backed by an injectable DNS resolver.
- Produces: `NewServer(Renderer) http.Handler` implementing the spec status codes and JSON body.

- [ ] **Step 1: Write failing API contract and single-worker tests**

```go
func TestServerReturnsBoundedJSONCard(t *testing.T) {
    renderer := &fakeRenderer{card: enrichment.Card{
        SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
        Title: "Sauvage Eau de Parfum Dior", PNG: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
    }, ok: true}
    request := httptest.NewRequest(http.MethodPost, "/v1/enrich", strings.NewReader(`{
        "brand":"Dior","name":"Sauvage","concentration":"edp"
    }`))
    response := httptest.NewRecorder()
    NewServer(renderer).ServeHTTP(response, request)
    if response.Code != http.StatusOK { t.Fatalf("status %d", response.Code) }
    var body struct { PNGBase64 string `json:"png_base64"` }
    if err := json.NewDecoder(response.Body).Decode(&body); err != nil { t.Fatal(err) }
    if decoded, _ := base64.StdEncoding.DecodeString(body.PNGBase64); !bytes.Equal(decoded, renderer.card.PNG) {
        t.Fatal("PNG mismatch")
    }
}

var request = enrichment.Request{
    Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
}

type fakeRenderer struct {
    card enrichment.Card
    ok bool
    err error
}

func (renderer *fakeRenderer) Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error) {
    return renderer.card, renderer.ok, renderer.err
}
func (renderer *fakeRenderer) Ready(context.Context) error { return renderer.err }

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
    go func() { _, _, err := renderer.Enrich(context.Background(), request); first <- err }()
    <-started
    if _, _, err := renderer.Enrich(context.Background(), request); !errors.Is(err, ErrBusy) {
        t.Fatalf("second error = %v, want ErrBusy", err)
    }
    close(release)
    if err := <-first; err != nil { t.Fatal(err) }
}

func TestNetworkGuardRejectsPrivateDNSAnswers(t *testing.T) {
    guard := NewNetworkGuard(fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}})
    target, _ := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html")
    if err := guard.Allow(context.Background(), target); !errors.Is(err, ErrUnsafeURL) {
        t.Fatalf("error = %v, want ErrUnsafeURL", err)
    }
}

type fakeResolver struct {
    addresses []net.IPAddr
    err error
}

func (resolver fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
    return resolver.addresses, resolver.err
}
```

Table-test server mappings: no match→404, `ErrBusy`→429, `ErrAccessChallenge`→503, deadline→504, body over 16 KB→400, decoded PNG over 5 MB→502. `/healthz` must call `Ready`.

- [ ] **Step 2: Run API tests and verify RED**

Run:

```bash
go test ./internal/fragrantica -run 'Server|BrowserRenderer' -count=1
```

Expected: missing renderer/server symbols.

- [ ] **Step 3: Implement server and Chromium renderer**

The server response type is exact:

```go
type enrichResponse struct {
    SourceURL string              `json:"source_url"`
    Title     string              `json:"title"`
    Accords   []enrichment.Accord `json:"accords"`
    PNGBase64 string              `json:"png_base64"`
}
```

`BrowserRenderer` navigates to:

```go
searchURL := "https://www.fragrantica.ru/search/?query=" + url.QueryEscape(
    strings.Join([]string{request.Brand, request.Name, request.Edition, string(request.Concentration)}, " "),
)
```

Capture rendered HTML with `chromedp.OuterHTML("html", &html, chromedp.ByQuery)`, use Task 4 parsers, navigate to the exact candidate, and parse the product. Render a local escaped HTML card containing the Fragrantica bottle image and extracted accord bars, wait for the image to complete, then use `chromedp.Screenshot("#perfume-finder-card", &png, chromedp.ByQuery)`.

Enable DevTools request interception before navigation. Continue requests only when scheme is HTTPS, the normalized hostname is one of `fragrantica.ru`, `www.fragrantica.ru`, `beta.fragrantica.com`, or `fimgs.net`, and all resolver answers are public unicast IPs. Fail all other requests and re-check `page.Location` after every navigation.

Implement the guard independently from DevTools so DNS rules are unit-testable:

```go
type Resolver interface {
    LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type NetworkGuard struct { resolver Resolver }

func (guard NetworkGuard) Allow(ctx context.Context, target *url.URL) error {
    if target.Scheme != "https" || !allowedFragranticaHost(target.Hostname()) { return ErrUnsafeURL }
    addresses, err := guard.resolver.LookupIPAddr(ctx, target.Hostname())
    if err != nil || len(addresses) == 0 { return ErrUnsafeURL }
    for _, address := range addresses {
        ip := address.IP
        if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
            return ErrUnsafeURL
        }
    }
    return nil
}
```

Use the same guard for initial navigation, redirect targets, product candidates, and bottle assets. Cache successful host resolutions only for the lifetime of one lookup.

`cmd/fragranticaenricher/main.go` starts one reusable browser allocator, listens on `:8081`, and shuts the HTTP server and Chrome down on SIGINT/SIGTERM. When invoked as `fragranticaenricher healthcheck`, it performs a bounded request to `http://127.0.0.1:8081/healthz` and exits non-zero unless browser readiness returns HTTP 200.

- [ ] **Step 4: Run sidecar package tests and verify GREEN**

Run:

```bash
go test -race ./internal/fragrantica ./cmd/fragranticaenricher -count=1
```

Expected: PASS without opening the network. Add an opt-in test guarded by `FRAGRANTICA_LIVE=1`; it is skipped by default and treats a detected challenge as a reported skip, never as a bypass attempt.

- [ ] **Step 5: Commit**

```bash
git add internal/fragrantica cmd/fragranticaenricher
git commit -m "feat: add Chromium Fragrantica sidecar"
```

---

### Task 6: Bounded Bot Client and Cached Enrichment Service

**Files:**
- Create: `internal/enrichment/client.go`
- Create: `internal/enrichment/client_test.go`
- Create: `internal/enrichment/service.go`
- Create: `internal/enrichment/service_test.go`

**Interfaces:**
- Produces: `NewHTTPClient(baseURL string, client *http.Client) (*HTTPClient, error)`.
- Produces: `HTTPClient.Enrich(context.Context, Request) (Card, bool, error)`.
- Produces: `NewCachedService(remote Service, cache Cache) Service`.
- Consumes: `storage.EnrichmentCache` through `Cache.Get/Put`.

- [ ] **Step 1: Write failing status, validation, and caching tests**

```go
func TestHTTPClientRejectsInvalidPNG(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        _ = json.NewEncoder(w).Encode(map[string]any{
            "source_url": "https://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html",
            "title": "Sauvage Dior", "png_base64": base64.StdEncoding.EncodeToString([]byte("not-png")),
        })
    }))
    defer server.Close()
    client, _ := NewHTTPClient(server.URL, server.Client())
    if _, _, err := client.Enrich(context.Background(), request); !errors.Is(err, ErrInvalidImage) {
        t.Fatalf("error = %v", err)
    }
}

var request = Request{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}
var wantedCard = Card{
    SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
    Title: "Sauvage Eau de Parfum Dior",
    PNG: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
}

func TestCachedServiceUsesFreshCardWithoutRemoteCall(t *testing.T) {
    cache := &fakeCache{card: wantedCard, ok: true}
    remote := &fakeService{}
    got, ok, err := NewCachedService(remote, cache).Enrich(context.Background(), request)
    if err != nil || !ok || remote.calls != 0 || !reflect.DeepEqual(got, wantedCard) {
        t.Fatalf("got %+v %v %v calls=%d", got, ok, err, remote.calls)
    }
}

type fakeService struct {
    card Card
    ok bool
    err error
    calls int
}

func (service *fakeService) Enrich(context.Context, Request) (Card, bool, error) {
    service.calls++
    return service.card, service.ok, service.err
}

type fakeCache struct {
    card Card
    ok bool
    err error
    puts int
}

func (cache *fakeCache) Get(context.Context, Request) (Card, bool, error) {
    return cache.card, cache.ok, cache.err
}
func (cache *fakeCache) Put(context.Context, Request, Card) error {
    cache.puts++
    return cache.err
}
```

Test exact status mapping, 16 KB request/7 MB response bounds, base64 decode failure, PNG signature, 5 MB decoded limit, safe Fragrantica source URL, cache miss/write, and cache-write failure not discarding a valid remote card.

- [ ] **Step 2: Run enrichment tests and verify RED**

Run:

```bash
go test ./internal/enrichment -count=1
```

Expected: missing client/service symbols.

- [ ] **Step 3: Implement the minimal client and decorator**

Use `io.LimitReader(response.Body, 7<<20)` before JSON decoding. Require response content type `application/json`, then accept only decoded bytes beginning with the eight-byte PNG signature and only canonical source URLs on Fragrantica hosts. Treat `404` as `(Card{}, false, nil)`; all other non-200 statuses return typed errors.

```go
func (service *cachedService) Enrich(ctx context.Context, request Request) (Card, bool, error) {
    if card, ok, err := service.cache.Get(ctx, request); err == nil && ok {
        return card, true, nil
    }
    card, ok, err := service.remote.Enrich(ctx, request)
    if err != nil || !ok { return card, ok, err }
    _ = service.cache.Put(ctx, request, card)
    return card, true, nil
}
```

- [ ] **Step 4: Run package tests and verify GREEN**

Run:

```bash
go test ./internal/enrichment ./internal/storage -count=1
```

Expected: PASS with no network access outside test servers.

- [ ] **Step 5: Commit**

```bash
git add internal/enrichment
git commit -m "feat: call and cache Fragrantica sidecar"
```

---

### Task 7: Telegram Photo Delivery and Price-First Orchestration

**Files:**
- Modify: `internal/telegram/handler.go`
- Modify: `internal/telegram/handler_test.go`
- Modify: `internal/telegram/transport.go`
- Create: `internal/telegram/transport_test.go`
- Modify: `cmd/perfumebot/main.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Extends: `telegram.Message` with `PhotoPNG []byte` and `Caption string`.
- Extends: `NewHandler(..., enricher enrichment.Service)` and `NewTransport(..., enricher enrichment.Service)`; nil disables enrichment.
- Adds: `config.Config.FragranticaEnricherURL string` from `FRAGRANTICA_ENRICHER_URL`.

- [ ] **Step 1: Write failing price-first, photo, fallback, and stale tests**

```go
func TestPriceResultIsSentBeforeFragranticaCard(t *testing.T) {
    enricher := &fakeEnricher{card: enrichment.Card{
        SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
        Title: "Sauvage Eau de Parfum Dior", PNG: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
    }, ok: true}
    messenger := &fakeMessenger{}
    h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), enricher)
    if err := runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл"); err != nil { t.Fatal(err) }
    if len(messenger.messages) != 2 || len(messenger.messages[0].PhotoPNG) != 0 || len(messenger.messages[1].PhotoPNG) == 0 {
        t.Fatalf("messages = %+v", messenger.messages)
    }
}

func TestEnrichmentFailureDoesNotReplacePriceResult(t *testing.T) {
    messenger := &fakeMessenger{}
    h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), &fakeEnricher{err: errors.New("challenge")})
    if err := runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл"); err != nil { t.Fatal(err) }
    if len(messenger.messages) != 1 || !strings.Contains(messenger.messages[0].Text, "Без доставки") {
        t.Fatalf("messages = %+v", messenger.messages)
    }
}

type fakeEnricher struct {
    card enrichment.Card
    ok bool
    err error
}

func (service *fakeEnricher) Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error) {
    return service.card, service.ok, service.err
}

func TestSupersededEnrichmentCannotSendPhoto(t *testing.T) {
    started, release := make(chan struct{}), make(chan struct{})
    enricher := &blockingEnricher{started: started, release: release, card: enrichment.Card{
        Title: "Sauvage Eau de Parfum Dior",
        SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
        PNG: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
    }}
    messenger := &fakeMessenger{}
    sessions := newMemorySessions()
    h := NewHandler(messenger, sessions, completeVariantSearcher(), enricher)

    firstDone := make(chan error, 1)
    go func() { firstDone <- runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл") }()
    <-started
    if err := h.HandleMessage(context.Background(), 42, "Tom Ford Ombre Leather EDP 100 мл"); err != nil {
        t.Fatal(err)
    }
    close(release)
    _ = <-firstDone
    for _, message := range messenger.messages {
        if len(message.PhotoPNG) > 0 && strings.Contains(message.Caption, "Sauvage") {
            t.Fatalf("stale photo sent: %+v", message)
        }
    }
}

type blockingEnricher struct {
    started chan struct{}
    release chan struct{}
    card enrichment.Card
    once sync.Once
}

func (service *blockingEnricher) Enrich(ctx context.Context, _ enrichment.Request) (enrichment.Card, bool, error) {
    service.once.Do(func() { close(service.started) })
    select {
    case <-service.release:
        return service.card, true, nil
    case <-ctx.Done():
        return enrichment.Card{}, false, ctx.Err()
    }
}

func completeVariantSearcher() Searcher {
    return fakeSearcher{result: search.Result{Offers: []domain.Offer{{
        ShopID: "orental", Brand: "Dior", Name: "Sauvage",
        Concentration: domain.ConcentrationEDP, VolumeMicroliters: 100000,
        Kind: domain.ProductKindRetail, InStock: true, PriceKopecks: 1000000,
    }}}}
}

func runCompleteQuery(h *Handler, chatID int64, query string) error {
    if err := h.HandleMessage(context.Background(), chatID, query); err != nil { return err }
    session, ok, err := h.sessions.Load(context.Background(), chatID)
    if err != nil { return err }
    if !ok { return errors.New("session not found") }
    return h.HandleCallback(context.Background(), chatID, callbackData(session.ID, "kind", "retail"))
}
```

Make `fakeMessenger` safe for the new race tests:

```go
type fakeMessenger struct {
    mu sync.Mutex
    messages []Message
}

func (m *fakeMessenger) Send(_ context.Context, _ int64, message Message) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.messages = append(m.messages, message)
    return nil
}
```

For transport, inject a fake Telegram API server through the bot client options or extract `sendText`/`sendPhoto` methods behind a minimal sender interface. Assert multipart upload filename `fragrantica.png`, PNG bytes, and caption; do not assert private token/update payloads.

- [ ] **Step 2: Run Telegram/config tests and verify RED**

Run:

```bash
go test -race ./internal/telegram ./internal/config -count=1
```

Expected: missing photo fields, enricher constructor arguments, and config field.

- [ ] **Step 3: Implement photo transport and orchestration**

Transport branch:

```go
if len(message.PhotoPNG) > 0 {
    _, err := t.bot.SendPhoto(ctx, &bot.SendPhotoParams{
        ChatID: chatID,
        Photo: &models.InputFileUpload{
            Filename: "fragrantica.png",
            Data: bytes.NewReader(message.PhotoPNG),
        },
        Caption: message.Caption,
    })
    return err
}
```

After exact price results are sent, call enrichment with the still-owned `searchCtx`. Check ownership again before photo upload. Ignore enrichment/photo errors after logging only sanitized fields (`chat_id`, outcome, duration), then finish ownership.

Wire an optional service in `cmd/perfumebot/main.go`:

```go
var enricher enrichment.Service
if cfg.FragranticaEnricherURL != "" {
    remote, err := enrichment.NewHTTPClient(cfg.FragranticaEnricherURL, &http.Client{Timeout: 15 * time.Second})
    if err != nil { return err }
    enricher = enrichment.NewCachedService(remote, storage.NewEnrichmentCache(db, 7*24*time.Hour, clock))
}
```

- [ ] **Step 4: Run Telegram/config tests and verify GREEN**

Run:

```bash
go test -race ./internal/telegram ./internal/config ./internal/enrichment -count=1
```

Expected: PASS; price is always the first message and stale photos never appear.

- [ ] **Step 5: Commit**

```bash
git add internal/telegram cmd/perfumebot/main.go internal/config
git commit -m "feat: send Fragrantica cards after prices"
```

---

### Task 8: Sidecar Docker Deployment and Full Verification

**Files:**
- Create: `Dockerfile.enricher`
- Modify: `docker-compose.yml`
- Modify: `.env.example`
- Modify: `README.md`
- Create: `tests/fragrantica_sidecar_test.sh`
- Modify: `tests/container_startup_test.sh`

**Interfaces:**
- Produces: Compose service `fragrantica-enricher` at internal URL `http://fragrantica-enricher:8081`.
- Produces: bot env `FRAGRANTICA_ENRICHER_URL`.
- Preserves: existing `perfume-data` volume and non-root SQLite ownership fix.

- [ ] **Step 1: Write the failing Docker smoke test**

```sh
#!/bin/sh
set -eu

image="perfume-finder-enricher-test-$$"
container="perfume-finder-enricher-test-$$"
cleanup() {
    docker rm -f "$container" >/dev/null 2>&1 || true
    docker image rm -f "$image" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker build --quiet -f Dockerfile.enricher -t "$image" . >/dev/null
docker run -d --name "$container" --read-only --tmpfs /tmp:rw,noexec,nosuid,size=256m \
    --shm-size=128m --cap-drop=ALL --memory=512m "$image" >/dev/null

for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if docker exec "$container" /fragranticaenricher healthcheck; then exit 0; fi
    sleep 1
done
docker logs "$container"
exit 1
```

- [ ] **Step 2: Run smoke test and verify RED**

Run:

```bash
sh tests/fragrantica_sidecar_test.sh
```

Expected: FAIL because `Dockerfile.enricher` and the sidecar healthcheck command do not exist.

- [ ] **Step 3: Add the hardened image, Compose service, and deployment docs**

`Dockerfile.enricher`:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/fragranticaenricher ./cmd/fragranticaenricher

FROM alpine:3.23
RUN apk add --no-cache chromium ca-certificates && adduser -D -u 65532 enricher
COPY --from=build /out/fragranticaenricher /fragranticaenricher
ENV HOME=/tmp CHROME_PATH=/usr/bin/chromium-browser HTTP_ADDR=:8081
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD ["/fragranticaenricher", "healthcheck"]
ENTRYPOINT ["/fragranticaenricher"]
```

Compose service:

```yaml
  fragrantica-enricher:
    build:
      context: .
      dockerfile: Dockerfile.enricher
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=256m
    shm_size: 128m
    mem_limit: 512m
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
```

Add `FRAGRANTICA_ENRICHER_URL=http://fragrantica-enricher:8081` to the bot environment and `.env.example`. Do not add a host `ports` mapping or make bot health depend on sidecar health. Document that Fragrantica is best-effort and that `docker compose up -d --build` starts both services.

- [ ] **Step 4: Run all verification gates**

Run:

```bash
gofmt -w cmd internal
go test -race ./...
go vet ./...
docker compose config
sh tests/container_startup_test.sh
sh tests/fragrantica_sidecar_test.sh
docker build -t perfume-price-bot:test .
docker build -f Dockerfile.enricher -t perfume-fragrantica-enricher:test .
git diff --check
```

Expected: all commands exit 0. Default Go tests make no external Fragrantica requests. Inspect `docker compose config` to confirm no sidecar port is published and the existing `perfume-data` volume remains attached to the bot.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile.enricher docker-compose.yml .env.example README.md tests
git commit -m "deploy: add private Fragrantica sidecar"
```

- [ ] **Step 6: Final review and push preparation**

Run:

```bash
git status --short
git log --oneline origin/main..HEAD
```

Expected: clean worktree and the design commit plus eight focused implementation commits. Request code review against `origin/main`; fix every Critical/Important finding with a new failing regression test before pushing `feature/fragrantica-enrichment`.
