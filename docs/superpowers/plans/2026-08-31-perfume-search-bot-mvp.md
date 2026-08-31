# Perfume Search Bot MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go Telegram bot that compares exact perfume variants across Randewoo, Allure Parfum, Orental, Духи.рф, and Aroma Butik and runs in Docker on one Yandex Cloud VPS.

**Architecture:** A long-polling Telegram transport builds a canonical query, then a coordinator calls five isolated `ShopAdapter` implementations concurrently. Adapters normalize store-specific search and product pages into `domain.Offer`; an exact matcher groups and sorts results, while SQLite provides a 15-minute cache, conversational state, and persisted health history.

**Tech Stack:** Go 1.26, standard `net/http` and `log/slog`, `goquery`, `go-telegram/bot`, `modernc.org/sqlite`, `golang.org/x/sync/errgroup`, `golang.org/x/time/rate`, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-08-31-perfume-price-bot-design.md`

## Global Constraints

- Enabled stores are exactly `randewoo.ru`, `allureparfum.ru`, `orental.ru`, `духи.рф`, and `aroma-butik.ru`.
- Compare product prices in rubles; do not calculate or display delivery costs.
- Final comparison requires the same brand, fragrance/edition, concentration, volume, and product kind.
- Product kinds are retail bottle, tester, decant, miniature, sample, and all kinds; all-kind results are grouped, never mixed.
- Per-store timeout is 8 seconds; one adapter failure must not cancel successful adapters.
- Allow at most 20 concurrent adapter calls globally and 2 concurrent calls per store.
- Allow 10 user searches per minute with burst 3; superseding a search cancels the previous search for that chat.
- Cache TTL is 15 minutes; expired prices are not presented as live.
- Health probes run every 6 hours and mark `down` only after 3 consecutive failures.
- The primary container is Go-only; do not add a browser runtime unless direct HTTP/AJAX access is proven insufficient.
- Telegram credentials come only from `TELEGRAM_BOT_TOKEN`; never log tokens, cookies, or full Telegram updates.
- Use TDD for every task and commit each independently testable deliverable.

## File Map

- `go.mod`, `go.sum` — module and dependency lock.
- `internal/domain/query.go` — canonical query, concentration, and product-kind types.
- `internal/domain/offer.go` — normalized store offer.
- `internal/domain/normalize.go` — text, concentration, volume, and product-kind normalization.
- `internal/search/matcher.go` — strict equivalence rules.
- `internal/search/ranker.go` — grouping and price sorting.
- `internal/search/coordinator.go` — concurrent adapter orchestration and partial failures.
- `internal/shop/adapter.go` — adapter and health contracts.
- `internal/shop/httpx/client.go` — bounded HTTP requests and document helpers.
- `internal/shop/{allure,orental,aromabutik,duhirf,randewoo}/` — one isolated adapter per store.
- `internal/storage/sqlite.go` — SQLite connection and embedded migrations.
- `internal/storage/cache.go` — 15-minute search cache.
- `internal/storage/session.go` — Telegram selection state.
- `internal/storage/health.go` — persisted health results.
- `internal/health/scheduler.go` — six-hour probes and three-failure transition.
- `internal/telegram/handler.go` — transport-independent conversation flow.
- `internal/telegram/render.go` — safe Telegram result rendering.
- `internal/telegram/transport.go` — `go-telegram/bot` integration.
- `internal/config/config.go` — environment parsing and exact defaults.
- `cmd/perfumebot/main.go` — composition root and graceful shutdown.
- `Dockerfile`, `docker-compose.yml`, `.env.example` — VPS packaging.
- `README.md` — local and VPS runbook.

---

### Task 1: Go Module and Canonical Domain Types

**Files:**
- Create: `go.mod`
- Create: `internal/domain/query.go`
- Create: `internal/domain/offer.go`
- Create: `internal/domain/normalize.go`
- Test: `internal/domain/normalize_test.go`

**Interfaces:**
- Produces: `domain.SearchQuery`, `domain.Offer`, `domain.ProductKind`, `domain.Concentration`, `domain.NormalizeText`, `domain.ParseConcentration`, `domain.ParseVolumeML`, `domain.ClassifyKind`.

- [ ] **Step 1: Initialize the module and write the failing normalization tests**

Run:

```bash
go mod init parfumes_finder
mkdir -p internal/domain
```

Create `internal/domain/normalize_test.go` with table tests asserting:

```go
func TestParseConcentration(t *testing.T) {
    cases := map[string]Concentration{
        "Dior Sauvage EDT": ConcentrationEDT,
        "Eau de Toilette":  ConcentrationEDT,
        "eau de parfum":    ConcentrationEDP,
        "Extrait de Parfum": ConcentrationExtrait,
        "Sauvage Elixir":   ConcentrationElixir,
    }
    for input, want := range cases {
        if got := ParseConcentration(input); got != want {
            t.Fatalf("ParseConcentration(%q) = %q, want %q", input, got, want)
        }
    }
}

func TestParseVolumeML(t *testing.T) {
    for input, want := range map[string]int{"100 мл": 100, "1.5 ml": 1, "8ML": 8} {
        if got := ParseVolumeML(input); got != want {
            t.Fatalf("ParseVolumeML(%q) = %d, want %d", input, got, want)
        }
    }
}

func TestClassifyKindIsConservative(t *testing.T) {
    cases := map[string]ProductKind{
        "тестер 100 мл": ProductKindTester,
        "пробник 1.5 мл": ProductKindSample,
        "миниатюра 8 мл": ProductKindMiniature,
        "отливант 10 мл": ProductKindDecant,
        "парфюмерная вода 100 мл в слюде": ProductKindRetail,
        "Sauvage 100 мл": ProductKindUnknown,
    }
    for input, want := range cases {
        if got := ClassifyKind(input); got != want {
            t.Fatalf("ClassifyKind(%q) = %q, want %q", input, got, want)
        }
    }
}
```

- [ ] **Step 2: Run the tests and verify the package does not compile**

Run: `go test ./internal/domain -v`

Expected: FAIL with undefined `Concentration`, `ProductKind`, and parser functions.

- [ ] **Step 3: Implement the exact domain types and parsers**

Define these public types without store-specific fields:

```go
type ProductKind string

const (
    ProductKindUnknown   ProductKind = "unknown"
    ProductKindRetail    ProductKind = "retail"
    ProductKindTester    ProductKind = "tester"
    ProductKindDecant    ProductKind = "decant"
    ProductKindMiniature ProductKind = "miniature"
    ProductKindSample    ProductKind = "sample"
    ProductKindAll       ProductKind = "all"
)

type Concentration string

const (
    ConcentrationUnknown Concentration = "unknown"
    ConcentrationEDT     Concentration = "edt"
    ConcentrationEDP     Concentration = "edp"
    ConcentrationParfum  Concentration = "parfum"
    ConcentrationExtrait Concentration = "extrait"
    ConcentrationCologne Concentration = "cologne"
    ConcentrationElixir  Concentration = "elixir"
)

type SearchQuery struct {
    Raw           string
    Brand         string
    Name          string
    Edition       string
    Concentration Concentration
    VolumeML      int
    Kind          ProductKind
}

type Offer struct {
    ShopID        string
    RawTitle      string
    Brand         string
    Name          string
    Edition       string
    Concentration Concentration
    VolumeML      int
    Kind          ProductKind
    PriceKopecks  int64
    InStock       bool
    URL           string
    RetrievedAt   time.Time
}
```

Implement normalization with lowercase Unicode text, `ё → е`, collapsed whitespace, explicit concentration patterns, decimal volume parsing, and ordered product-kind rules. Check `пробник` before volume-based guesses; never infer retail from volume alone.

- [ ] **Step 4: Run formatting and domain tests**

Run: `gofmt -w internal/domain && go test ./internal/domain -v`

Expected: PASS.

- [ ] **Step 5: Commit the domain foundation**

```bash
git add go.mod internal/domain
git commit -m "feat: add canonical perfume domain model"
```

### Task 2: Exact Variant Matcher and Ranker

**Files:**
- Create: `internal/search/matcher.go`
- Create: `internal/search/ranker.go`
- Test: `internal/search/matcher_test.go`
- Test: `internal/search/ranker_test.go`

**Interfaces:**
- Consumes: `domain.SearchQuery`, `domain.Offer`, `domain.ProductKind`.
- Produces: `search.SameVariant(query, offer) bool`, `search.GroupAndSort(query, offers) map[domain.ProductKind][]domain.Offer`.

- [ ] **Step 1: Write failing tests for confusing names and kind isolation**

The matcher table must include these cases:

```go
func TestSameVariant(t *testing.T) {
    query := domain.SearchQuery{Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeML: 100, Kind: domain.ProductKindRetail}
    base := domain.Offer{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeML: 100, Kind: domain.ProductKindRetail}

    cases := []struct {
        name string
        mutate func(*domain.Offer)
        want bool
    }{
        {name: "exact", mutate: func(*domain.Offer) {}, want: true},
        {name: "eau sauvage", mutate: func(o *domain.Offer) { o.Name = "Eau Sauvage" }, want: false},
        {name: "elixir", mutate: func(o *domain.Offer) { o.Concentration = domain.ConcentrationElixir }, want: false},
        {name: "sample", mutate: func(o *domain.Offer) { o.Kind = domain.ProductKindSample }, want: false},
        {name: "other volume", mutate: func(o *domain.Offer) { o.VolumeML = 60 }, want: false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := base
            tc.mutate(&got)
            if SameVariant(query, got) != tc.want {
                t.Fatalf("SameVariant() mismatch for %s", tc.name)
            }
        })
    }
}
```

Add a ranker test that supplies unsorted retail and tester offers and asserts separate ascending groups for `ProductKindAll`.

- [ ] **Step 2: Run the search tests and verify missing symbols**

Run: `go test ./internal/search -v`

Expected: FAIL with undefined `SameVariant` and `GroupAndSort`.

- [ ] **Step 3: Implement strict equality and explicit brand aliases**

Use one small alias table:

```go
var brandAliases = map[string]string{
    "dior":           "christian dior",
    "christian dior": "christian dior",
}
```

`SameVariant` must normalize brand/name/edition, require concentration and volume equality, and require kind equality unless the query kind is `ProductKindAll`. `GroupAndSort` must discard non-matches, group by actual offer kind, and use `sort.SliceStable` on `PriceKopecks`, then `ShopID`.

- [ ] **Step 4: Run the matcher and ranker tests**

Run: `gofmt -w internal/search && go test ./internal/search -v`

Expected: PASS.

- [ ] **Step 5: Commit matching and ranking**

```bash
git add internal/search
git commit -m "feat: match and rank exact perfume variants"
```

### Task 3: Adapter Contract and Partial-Result Coordinator

**Files:**
- Create: `internal/shop/adapter.go`
- Create: `internal/search/coordinator.go`
- Test: `internal/search/coordinator_test.go`

**Interfaces:**
- Produces: `shop.Adapter`, `shop.HealthResult`, `shop.HealthStatus`, `shop.ErrorKind`, `search.Cache`, `search.Coordinator.Search(ctx, query) search.Result`.
- Consumes later: concrete adapters and cache repository.

- [ ] **Step 1: Write a failing concurrency and partial-failure test**

Define fakes in the test and assert that a 20 ms success survives a failing adapter and a context-blocked adapter. Add a second test with 40 simultaneous searches that records active calls and asserts no more than 20 globally and 2 for one adapter ID:

```go
func TestCoordinatorReturnsPartialResults(t *testing.T) {
    good := fakeAdapter{id: "good", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) {
        return []domain.Offer{{ShopID: "good", PriceKopecks: 10000}}, nil
    }}
    bad := fakeAdapter{id: "bad", search: func(context.Context, domain.SearchQuery) ([]domain.Offer, error) {
        return nil, shop.NewError(shop.ErrorParse, errors.New("markup changed"))
    }}
    blocked := fakeAdapter{id: "slow", search: func(ctx context.Context, _ domain.SearchQuery) ([]domain.Offer, error) {
        <-ctx.Done()
        return nil, ctx.Err()
    }}
    c := NewCoordinator([]shop.Adapter{good, bad, blocked}, 20*time.Millisecond, nil)
    result := c.Search(context.Background(), domain.SearchQuery{Raw: "Dior Sauvage"})
    if len(result.Offers) != 1 || len(result.Failures) != 2 {
        t.Fatalf("got %d offers and %d failures", len(result.Offers), len(result.Failures))
    }
}
```

- [ ] **Step 2: Run the test and verify contract symbols are missing**

Run: `go test ./internal/search -run TestCoordinatorReturnsPartialResults -v`

Expected: FAIL with undefined adapter and coordinator types.

- [ ] **Step 3: Implement typed errors and isolated goroutines**

Define:

```go
type Adapter interface {
    ID() string
    Search(context.Context, domain.SearchQuery) ([]domain.Offer, error)
    Health(context.Context) HealthResult
}

type HealthStatus string

const (
    HealthHealthy    HealthStatus = "healthy"
    HealthRedirected HealthStatus = "redirected"
    HealthDegraded   HealthStatus = "degraded"
    HealthDown       HealthStatus = "down"
    HealthDisabled   HealthStatus = "disabled"
)

type HealthResult struct {
    Status      HealthStatus
    CanonicalURL string
    Err         error
    CheckedAt   time.Time
}

type Cache interface {
    Get(context.Context, domain.SearchQuery) ([]domain.Offer, bool, error)
    Put(context.Context, domain.SearchQuery, []domain.Offer) error
}

type ErrorKind string

const (
    ErrorTimeout ErrorKind = "timeout"
    ErrorAccess  ErrorKind = "access_denied"
    ErrorAntiBot ErrorKind = "anti_bot"
    ErrorParse   ErrorKind = "parse_failure"
)
```

Use `errgroup.WithContext` only for parent cancellation; each goroutine sends a result and returns `nil`, so one adapter error cannot cancel peers. Wrap each adapter call in its own `context.WithTimeout`. Gate calls with a global buffered semaphore of size 20 and one semaphore of size 2 per adapter ID; acquiring either semaphore must respect context cancellation.

- [ ] **Step 4: Run race-enabled coordinator tests**

Run: `gofmt -w internal/shop internal/search && go test -race ./internal/search ./internal/shop`

Expected: PASS with no race reports.

- [ ] **Step 5: Commit the coordinator**

```bash
git add internal/shop internal/search
git commit -m "feat: coordinate shop searches with partial failures"
```

### Task 4: SQLite Cache, Sessions, and Health Repository

**Files:**
- Create: `internal/storage/sqlite.go`
- Create: `internal/storage/migrations/001_core.sql`
- Create: `internal/storage/cache.go`
- Create: `internal/storage/session.go`
- Create: `internal/storage/health.go`
- Test: `internal/storage/storage_test.go`

**Interfaces:**
- Produces: `storage.Open(path)`, `storage.Cache.Get/Put`, `storage.Sessions.Load/Save/Delete`, `storage.Health.Record/Current` with probe kinds `homepage` and `search`.
- Consumes: JSON-serializable `domain.SearchQuery` and `[]domain.Offer`.

- [ ] **Step 1: Add SQLite and write failing repository tests**

Run: `go get modernc.org/sqlite`

Test with `t.TempDir()` and a controllable clock. Assertions must cover cache hit before 15 minutes, cache miss at 15 minutes, session round-trip, and health failure count increment.

```go
func TestCacheExpiresAtTTL(t *testing.T) {
    now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
    db := openTestDB(t)
    cache := NewCache(db, 15*time.Minute, func() time.Time { return now })
    query := domain.SearchQuery{Raw: "Dior Sauvage"}
    offers := []domain.Offer{{ShopID: "orental", PriceKopecks: 123400}}
    if err := cache.Put(context.Background(), query, offers); err != nil { t.Fatal(err) }
    now = now.Add(15 * time.Minute)
    if _, ok, err := cache.Get(context.Background(), query); err != nil || ok {
        t.Fatalf("expired cache returned ok=%v err=%v", ok, err)
    }
}
```

- [ ] **Step 2: Run storage tests and verify the repository is absent**

Run: `go test ./internal/storage -v`

Expected: FAIL with undefined storage constructors.

- [ ] **Step 3: Implement embedded migrations and repositories**

Create tables `search_cache`, `telegram_sessions`, `shop_health`, and `shops`. Use a deterministic cache key from normalized query JSON hashed with SHA-256. Store price payloads as JSON and delete expired entries on read. Store Telegram session stage plus canonical query JSON.

The `shop_health` table is keyed by `(shop_id, probe_kind)` so a generic homepage check cannot overwrite an adapter search check. The upsert must set `consecutive_failures = 0` on success and increment it on failure; status becomes `down` only when the new count reaches 3.

- [ ] **Step 4: Run storage tests with the race detector**

Run: `gofmt -w internal/storage && go test -race ./internal/storage -v`

Expected: PASS.

- [ ] **Step 5: Commit persistence**

```bash
git add go.mod go.sum internal/storage
git commit -m "feat: persist cache sessions and shop health"
```

### Task 5: Shared HTTP and Parser Test Harness

**Files:**
- Create: `internal/shop/httpx/client.go`
- Create: `internal/shop/httpx/document.go`
- Test: `internal/shop/httpx/client_test.go`

**Interfaces:**
- Produces: `httpx.Client.GetDocument`, `httpx.Client.PostForm`, `httpx.DecodeJSON`, response-size and status validation.
- Consumes: standard `*http.Client` for test injection.

- [ ] **Step 1: Add goquery and write failing HTTP tests**

Run: `go get github.com/PuerkitoBio/goquery`

Use `httptest.Server` to assert the client sends a stable `User-Agent`, rejects non-2xx responses, caps bodies at 5 MiB, and classifies `403` as `shop.ErrorAccess`.

- [ ] **Step 2: Run the test and verify helpers are missing**

Run: `go test ./internal/shop/httpx -v`

Expected: FAIL with undefined `Client` and helper methods.

- [ ] **Step 3: Implement bounded request helpers**

`GetDocument` returns a `*goquery.Document`; `PostForm` returns bounded bytes so JSON-wrapped HTML can be decoded by Духи.рф. Set `Accept-Language: ru-RU,ru;q=0.9` and the stable user agent `PerfumePriceBot/0.1`.

- [ ] **Step 4: Run HTTP tests**

Run: `gofmt -w internal/shop/httpx && go test -race ./internal/shop/httpx -v`

Expected: PASS.

- [ ] **Step 5: Commit HTTP infrastructure**

```bash
git add go.mod go.sum internal/shop/httpx
git commit -m "feat: add bounded shop HTTP client"
```

### Task 6: Allure Parfum Adapter

**Files:**
- Create: `internal/shop/allure/adapter.go`
- Create: `internal/shop/allure/parser.go`
- Create: `internal/shop/allure/testdata/search-dior-sauvage.html`
- Create: `internal/shop/allure/testdata/product-sauvage-2015.html`
- Test: `internal/shop/allure/parser_test.go`
- Test: `internal/shop/allure/adapter_test.go`

**Interfaces:**
- Produces: `allure.New(*httpx.Client, func() time.Time) shop.Adapter` with ID `allure`.
- Search URL: `https://allureparfum.ru/search/?q=<query>`.

- [ ] **Step 1: Capture immutable fixtures and write failing parser tests**

```bash
mkdir -p internal/shop/allure/testdata
curl -LsS --compressed 'https://allureparfum.ru/search/?q=Dior%20Sauvage' -o internal/shop/allure/testdata/search-dior-sauvage.html
curl -LsS --compressed 'https://allureparfum.ru/katalog/muzhskaya-parfyumeriya/christian-dior/sauvage-2015.html' -o internal/shop/allure/testdata/product-sauvage-2015.html
```

The tests must assert that search parsing finds the `Sauvage 2015` product URL and product parsing emits at least one offer with `Brand=Christian Dior`, `Name=Sauvage`, `Edition=2015`, a positive volume, a positive price, and a non-unknown kind.

- [ ] **Step 2: Run the adapter tests and verify missing parsers**

Run: `go test ./internal/shop/allure -v`

Expected: FAIL with undefined `parseSearch` and `parseProduct`.

- [ ] **Step 3: Implement search, product-page variants, and health**

Parse product links from the result cards, then parse every displayed package/volume row on the product page. Treat explicit `пробник`, `миниатюра`, `отливант`, and `тестер` labels conservatively; retail requires an explicit retail/package signal. Convert rubles to kopecks without floating-point arithmetic.

`Health` performs the `Dior Sauvage` search and succeeds only if a `Sauvage` product link is parsed.

- [ ] **Step 4: Run fixture and local HTTP integration tests**

Run: `gofmt -w internal/shop/allure && go test -race ./internal/shop/allure -v`

Expected: PASS.

- [ ] **Step 5: Commit Allure support**

```bash
git add internal/shop/allure
git commit -m "feat: add Allure Parfum adapter"
```

### Task 7: Orental Adapter

**Files:**
- Create: `internal/shop/orental/adapter.go`
- Create: `internal/shop/orental/parser.go`
- Create: `internal/shop/orental/testdata/search-dior-sauvage.html`
- Create: `internal/shop/orental/testdata/product-sauvage.html`
- Test: `internal/shop/orental/parser_test.go`
- Test: `internal/shop/orental/adapter_test.go`

**Interfaces:**
- Produces: `orental.New(*httpx.Client, func() time.Time) shop.Adapter` with ID `orental`.
- Search URL: `https://www.orental.ru/search/?q=<query>`.

- [ ] **Step 1: Capture fixtures and write failing tests for exact offers**

```bash
mkdir -p internal/shop/orental/testdata
curl -LsS --compressed 'https://www.orental.ru/search/?q=Dior%20Sauvage' -o internal/shop/orental/testdata/search-dior-sauvage.html
curl -LsS --compressed 'https://www.orental.ru/men/christian-dior/sauvage/' -o internal/shop/orental/testdata/product-sauvage.html
```

Assert that search parsing distinguishes `/sauvage/` from `/eau-sauvage/`, and product parsing emits separate offers for displayed volumes rather than reusing the search-page range minimum.

- [ ] **Step 2: Run Orental tests and verify parser failure**

Run: `go test ./internal/shop/orental -v`

Expected: FAIL with undefined adapter/parser functions.

- [ ] **Step 3: Implement result-card and offer parsing**

Use the product URL and visible variant controls as the source of truth. Reject cards whose canonical name is `Eau Sauvage`, `Sauvage Elixir`, `Sauvage Parfum`, or another edition when the query requests plain `Sauvage EDT`.

- [ ] **Step 4: Run Orental tests**

Run: `gofmt -w internal/shop/orental && go test -race ./internal/shop/orental -v`

Expected: PASS.

- [ ] **Step 5: Commit Orental support**

```bash
git add internal/shop/orental
git commit -m "feat: add Orental adapter"
```

### Task 8: Aroma Butik Adapter

**Files:**
- Create: `internal/shop/aromabutik/adapter.go`
- Create: `internal/shop/aromabutik/parser.go`
- Create: `internal/shop/aromabutik/testdata/search-dior-sauvage.html`
- Create: `internal/shop/aromabutik/testdata/product-sauvage-2015.html`
- Test: `internal/shop/aromabutik/parser_test.go`
- Test: `internal/shop/aromabutik/adapter_test.go`

**Interfaces:**
- Produces: `aromabutik.New(*httpx.Client, func() time.Time) shop.Adapter` with ID `aromabutik`.
- Search URL: `https://www.aroma-butik.ru/advanced_search_result.php?keywords=<query>`.

- [ ] **Step 1: Capture fixtures and write failing variant tests**

```bash
mkdir -p internal/shop/aromabutik/testdata
curl -LsS --compressed 'https://www.aroma-butik.ru/advanced_search_result.php?keywords=Dior%20Sauvage' -o internal/shop/aromabutik/testdata/search-dior-sauvage.html
curl -LsS --compressed 'https://www.aroma-butik.ru/product/christian-dior-sauvage-2015/' -o internal/shop/aromabutik/testdata/product-sauvage-2015.html
```

Assert that the parser excludes `Areej Diorit Sovgee (по мотивам Sauvage)` and emits separate positive-price offers for each explicit Aroma Butik variant.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/shop/aromabutik -v`

Expected: FAIL with undefined parser functions.

- [ ] **Step 3: Implement the adapter and dot-separated ruble parser**

Handle site prices such as `29.600 ₽` as 29,600 rubles, not 29.6. Do not use `strconv.ParseFloat` for monetary strings; strip Unicode spaces and punctuation according to Russian thousands separators.

- [ ] **Step 4: Run Aroma Butik tests**

Run: `gofmt -w internal/shop/aromabutik && go test -race ./internal/shop/aromabutik -v`

Expected: PASS.

- [ ] **Step 5: Commit Aroma Butik support**

```bash
git add internal/shop/aromabutik
git commit -m "feat: add Aroma Butik adapter"
```

### Task 9: Духи.рф AJAX Adapter

**Files:**
- Create: `internal/shop/duhirf/adapter.go`
- Create: `internal/shop/duhirf/parser.go`
- Create: `internal/shop/duhirf/testdata/live-search.json`
- Create: `internal/shop/duhirf/testdata/product-sauvage-2015.html`
- Test: `internal/shop/duhirf/parser_test.go`
- Test: `internal/shop/duhirf/adapter_test.go`

**Interfaces:**
- Produces: `duhirf.New(*httpx.Client, func() time.Time) shop.Adapter` with ID `duhirf`.
- Search contract: `POST https://xn--d1ai6ai.xn--p1ai/index.php?section=6` with form fields `cmd=live_search` and `text=<query>`; response is a JSON string containing HTML.

- [ ] **Step 1: Capture AJAX and product fixtures and write failing tests**

```bash
mkdir -p internal/shop/duhirf/testdata
curl -LsS --compressed -X POST -d 'cmd=live_search&text=dior sauvage' 'https://xn--d1ai6ai.xn--p1ai/index.php?section=6' -o internal/shop/duhirf/testdata/live-search.json
curl -LsS --compressed 'https://xn--d1ai6ai.xn--p1ai/catalog/men/Christian-Dior/Sauvage-2015' -o internal/shop/duhirf/testdata/product-sauvage-2015.html
```

Assert that JSON decoding produces HTML, `.term_res_prod` parsing finds product ID `16031`, and the protocol-relative URL becomes HTTPS on the Punycode host.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/shop/duhirf -v`

Expected: FAIL with undefined live-search parser.

- [ ] **Step 3: Implement AJAX search and product variants**

POST form data, decode the JSON string, parse the contained HTML with goquery, and open only product candidates whose normalized brand/name match the query. Parse the product page's explicit package rows into offers; keep Unicode `Духи.рф` for display and Punycode for network URLs.

- [ ] **Step 4: Run Духи.рф tests**

Run: `gofmt -w internal/shop/duhirf && go test -race ./internal/shop/duhirf -v`

Expected: PASS.

- [ ] **Step 5: Commit Духи.рф support**

```bash
git add internal/shop/duhirf
git commit -m "feat: add Duhi RF adapter"
```

### Task 10: Randewoo Diginetica Adapter

**Files:**
- Create: `internal/shop/randewoo/adapter.go`
- Create: `internal/shop/randewoo/parser.go`
- Create: `internal/shop/randewoo/testdata/autocomplete-dior-sauvage.json`
- Create: `internal/shop/randewoo/testdata/product-sauvage-2015.html`
- Test: `internal/shop/randewoo/parser_test.go`
- Test: `internal/shop/randewoo/adapter_test.go`

**Interfaces:**
- Produces: `randewoo.New(*httpx.Client, func() time.Time) shop.Adapter` with ID `randewoo`.
- Search contract: `GET https://autocomplete.diginetica.net/autocomplete?st=<query>&apiKey=594L68C4CP`.

- [ ] **Step 1: Capture fixtures and write failing API tests**

```bash
mkdir -p internal/shop/randewoo/testdata
curl -LsS --compressed 'https://autocomplete.diginetica.net/autocomplete?st=Dior%20Sauvage&apiKey=594L68C4CP' -o internal/shop/randewoo/testdata/autocomplete-dior-sauvage.json
curl -LsS --compressed 'https://randewoo.ru/product/christian-dior-sauvage-2015?preferred=437349' -o internal/shop/randewoo/testdata/product-sauvage-2015.html
```

Assert that autocomplete parsing keeps `Christian Dior / Sauvage 2015`, rejects the shower-gel category, and treats autocomplete prices and parallel attribute arrays as candidate metadata only.

- [ ] **Step 2: Run Randewoo tests and verify failure**

Run: `go test ./internal/shop/randewoo -v`

Expected: FAIL with undefined API response and product parser.

- [ ] **Step 3: Implement Diginetica discovery plus product-page verification**

Decode `products`, `categories`, `attributes`, and `link_url`. Never zip `attributes.price`, `attributes.объем`, and `attributes.тип товара`; their ordering is not a stable variant contract. Open candidate `link_url` pages and emit only variants whose volume, kind, and price are associated in the product-page markup. Keep the public API key as a named constant with a contract test; it is site configuration, not a secret.

- [ ] **Step 4: Run Randewoo tests**

Run: `gofmt -w internal/shop/randewoo && go test -race ./internal/shop/randewoo -v`

Expected: PASS.

- [ ] **Step 5: Commit Randewoo support**

```bash
git add internal/shop/randewoo
git commit -m "feat: add Randewoo adapter"
```

### Task 11: Health Scheduler and Cached Coordinator

**Files:**
- Modify: `internal/search/coordinator.go`
- Create: `internal/health/scheduler.go`
- Test: `internal/search/cache_test.go`
- Test: `internal/health/scheduler_test.go`

**Interfaces:**
- Consumes: all adapters, `storage.Cache`, `storage.Health`.
- Produces: six-hour scheduler and cache-aware `Coordinator.Search`.

- [ ] **Step 1: Write failing cache and three-failure health tests**

Use fake clocks/tickers. Assert that a second identical search within 15 minutes does not call adapters, and that the health repository remains `degraded` after failures 1 and 2 and becomes `down` after failure 3.

- [ ] **Step 2: Run tests and verify missing scheduler behavior**

Run: `go test ./internal/search ./internal/health -v`

Expected: FAIL on missing cache integration and scheduler.

- [ ] **Step 3: Implement cache-first search and scheduled health probes**

Check cache before starting goroutines, cache only normalized successful offers, and include fresh adapter failures in the returned result. The scheduler records adapter probes with `probe_kind=search`, runs immediately at startup, then every six hours, with parent-context cancellation.

- [ ] **Step 4: Run race-enabled tests**

Run: `gofmt -w internal/search internal/health && go test -race ./internal/search ./internal/health -v`

Expected: PASS.

- [ ] **Step 5: Commit health and caching integration**

```bash
git add internal/search internal/health
git commit -m "feat: cache searches and monitor shop health"
```

### Task 12: Telegram Selection Flow and Safe Rendering

**Files:**
- Create: `internal/telegram/handler.go`
- Create: `internal/telegram/render.go`
- Create: `internal/telegram/ratelimit.go`
- Create: `internal/telegram/transport.go`
- Test: `internal/telegram/handler_test.go`
- Test: `internal/telegram/render_test.go`
- Test: `internal/telegram/ratelimit_test.go`

**Interfaces:**
- Consumes: `search.Coordinator`, `storage.Sessions`, domain types.
- Produces: free-text query flow, inline callbacks, grouped result messages.

- [ ] **Step 1: Add Telegram dependency and write failing flow tests**

Run: `go get github.com/go-telegram/bot golang.org/x/time/rate`

Test a full fake-messenger sequence:

1. message `Dior Sauvage` returns fragrance choices;
2. choice `Sauvage EDT` returns volumes;
3. choice `100 мл` returns product kinds;
4. choice `all` calls the coordinator;
5. response groups retail/tester/sample and includes a warning for a timed-out store.

Add a renderer test containing `<script>`, `&`, and a `javascript:` URL; assert text is escaped and the unsafe URL is omitted.

Add rate-limit tests with a fake clock: the first 3 searches pass immediately, the 4th immediate search receives a concise retry message, and one token becomes available after 6 seconds. Add a supersession test where a second query for the same chat cancels the first coordinator context.

- [ ] **Step 2: Run Telegram tests and verify missing handler**

Run: `go test ./internal/telegram -v`

Expected: FAIL with undefined handler and renderer.

- [ ] **Step 3: Implement explicit state transitions**

Use stages `choose_fragrance`, `choose_concentration`, `choose_volume`, `choose_kind`, and `searching`. Encode callback data as short opaque session IDs plus option IDs, not raw URLs. If the initial text already contains a unique complete variant, skip resolved stages. Keep one `rate.Limiter` per chat at `rate.Every(6*time.Second)` with burst 3 and evict idle entries after one hour. Keep one active search cancellation function per chat; replace and call it when a newer query starts.

Render one group per product kind, ascending prices, store link, stock state, check time, and `Без доставки`. Render adapter failures after successful offers.

- [ ] **Step 4: Run Telegram tests**

Run: `gofmt -w internal/telegram && go test -race ./internal/telegram -v`

Expected: PASS.

- [ ] **Step 5: Commit Telegram behavior**

```bash
git add go.mod go.sum internal/telegram
git commit -m "feat: add Telegram perfume search flow"
```

### Task 13: Configuration, Composition, Docker, and End-to-End Smoke Test

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Create: `cmd/perfumebot/main.go`
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `README.md`
- Test: `internal/app/smoke_test.go`
- Test: `internal/shop/live/live_test.go`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: every adapter, coordinator, SQLite repositories, scheduler, Telegram transport.
- Produces: runnable `perfumebot` process and Docker image.

- [ ] **Step 1: Write failing config and composition smoke tests**

Config tests must assert exact defaults:

```go
func TestLoadDefaults(t *testing.T) {
    t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
    cfg, err := Load()
    if err != nil { t.Fatal(err) }
    if cfg.SearchTimeout != 8*time.Second || cfg.CacheTTL != 15*time.Minute || cfg.HealthInterval != 6*time.Hour || cfg.GlobalConcurrency != 20 || cfg.PerStoreConcurrency != 2 {
        t.Fatalf("unexpected defaults: %+v", cfg)
    }
}
```

The app smoke test composes fake adapters and a temporary SQLite DB, sends one handler query, and asserts a sorted response without network access.

- [ ] **Step 2: Run config and smoke tests and verify failure**

Run: `go test ./internal/config ./internal/app -v`

Expected: FAIL with missing config and composition package.

- [ ] **Step 3: Implement the composition root and graceful shutdown**

Wire the five adapters in the approved order, start the health scheduler, start Telegram long polling, and stop on `SIGINT`/`SIGTERM`. Validate `TELEGRAM_BOT_TOKEN` and `DB_PATH` at startup. Add a local `/healthz` HTTP endpoint that returns success only after SQLite opens and the Telegram loop starts. Add `/perfumebot healthcheck`, which calls `http://127.0.0.1:8080/healthz` and exits nonzero on failure so the distroless image needs no shell or curl.

- [ ] **Step 4: Add a reproducible Docker deployment**

Use this shape:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/perfumebot ./cmd/perfumebot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/perfumebot /perfumebot
VOLUME ["/data"]
ENV DB_PATH=/data/perfumes.db
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD ["/perfumebot", "healthcheck"]
ENTRYPOINT ["/perfumebot"]
```

Compose must mount `perfume-data:/data`, use `restart: unless-stopped`, and load `.env`; Docker uses the binary healthcheck from the image. `.env.example` contains only `TELEGRAM_BOT_TOKEN=` and non-secret defaults.

- [ ] **Step 5: Run all verification commands**

Run:

```bash
gofmt -w cmd internal
go test -race ./...
go vet ./...
docker compose config
docker build -t perfume-price-bot:test .
```

Expected: all Go tests PASS, vet exits 0, Compose validates, and the image builds.

- [ ] **Step 6: Run opt-in live smoke tests**

Create `internal/shop/live/live_test.go` with `//go:build live`. Instantiate all five real adapters, search `Dior Sauvage`, and validate that every returned candidate has a safe HTTPS URL, non-empty brand/name, positive price after product-page expansion, and no exact-match contamination from `Eau Sauvage` or shower gel.

Run: `go test -tags=live ./internal/shop/... -run TestLiveSearchDiorSauvage -v`

Expected: every enabled adapter returns at least one valid candidate; any typed anti-bot/access error fails the live smoke test with a clear store diagnostic. No parser panic, zero price, unsafe URL, or mixed variant is accepted.

- [ ] **Step 7: Commit the runnable MVP**

```bash
git add cmd internal Dockerfile docker-compose.yml .env.example README.md .gitignore go.mod go.sum
git commit -m "feat: ship Dockerized perfume price bot MVP"
```
