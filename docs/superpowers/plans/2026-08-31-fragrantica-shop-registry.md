# Fragrantica Shop Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import all 16 pages of Fragrantica topic `235155` into an auditable shop registry without automatically trusting or enabling newly mentioned domains.

**Architecture:** A separate Go/Chromium capture command saves the 16 rendered topic pages because direct HTTP access currently returns `403`; it stops rather than bypassing CAPTCHAs. A fixture-tested offline importer identifies the opening whitelist separately from later discussion, normalizes external domains, and records source evidence. SQLite appends idempotent evidence while preserving manual trust and enabled states; a CLI exposes explicit import and inspection commands.

**Tech Stack:** Go 1.26, `chromedp` in an optional capture image, `goquery`, `modernc.org/sqlite`, `golang.org/x/net/idna`.

**Spec:** `docs/superpowers/specs/2026-08-31-perfume-price-bot-design.md`

## Global Constraints

- Parse the complete 16-page topic `https://www.fragrantica.ru/board/viewtopic.php?id=235155`.
- Treat domains from the opening post's `Адреса проверенных магазинов` section as historical whitelist evidence, not current availability proof.
- Treat later domains as candidates unless a human changes their trust state.
- Forum imports may append evidence and aliases but must never change `trust_state` or `enabled`.
- Store Unicode domains for display and Punycode domains for network identity.
- Preserve page, post URL, observed date, evidence kind, and excerpt for every imported fact.
- Do not bypass Fragrantica access controls or CAPTCHAs.
- Direct HTTP returned `403` on 2026-08-31; import from browser-captured HTML snapshots rather than retrying blocked requests.
- Use fixtures for default tests; live import is an explicit command.
- This plan starts after `2026-08-31-perfume-search-bot-mvp.md` is complete because it extends SQLite, health persistence, and the bot composition root.

## File Map

- `internal/registry/model.go` — trust states and evidence records.
- `internal/registry/domain.go` — URL extraction, Unicode/Punycode normalization, and exclusions.
- `internal/capture/fragrantica.go` — rendered-page capture with access-control detection.
- `cmd/fragranticacapture/main.go` — explicit 16-page snapshot command.
- `internal/registry/parser.go` — opening-post and discussion parsing.
- `internal/registry/importer.go` — snapshot-directory orchestration and idempotency keys.
- `internal/registry/testdata/page-01.html`, `page-16.html` — representative fixtures.
- `internal/storage/migrations/002_registry.sql` — registry evidence and aliases.
- `internal/storage/registry.go` — atomic evidence persistence without trust mutation.
- `cmd/shopregistry/main.go` — explicit import and inspection CLI.
- `Dockerfile.capture` — optional Chromium capture image, separate from the bot image.
- `README.md` — registry operations and safety rules.

---

### Task 1: Domain Normalization and Evidence Model

**Files:**
- Create: `internal/registry/model.go`
- Create: `internal/registry/domain.go`
- Test: `internal/registry/domain_test.go`

**Interfaces:**
- Produces: `registry.TrustState`, `registry.EvidenceKind`, `registry.Shop`, `registry.Evidence`, `registry.NormalizeDomain(rawURL) (display, network string, error)`.

- [ ] **Step 1: Add IDNA support and write failing domain tests**

Run: `go get golang.org/x/net/idna`

Create tests:

```go
func TestNormalizeDomain(t *testing.T) {
    cases := []struct{ input, display, network string }{
        {"https://www.randewoo.ru/path", "randewoo.ru", "randewoo.ru"},
        {"https://духи.рф/catalog", "духи.рф", "xn--d1ai6ai.xn--p1ai"},
        {"http://SCENTE.RU/", "scente.ru", "scente.ru"},
    }
    for _, tc := range cases {
        display, network, err := NormalizeDomain(tc.input)
        if err != nil || display != tc.display || network != tc.network {
            t.Fatalf("NormalizeDomain(%q) = %q, %q, %v", tc.input, display, network, err)
        }
    }
}
```

Add rejection cases for `javascript:`, Fragrantica internal links, malformed URLs, and image hosts.

- [ ] **Step 2: Run the tests and verify missing registry symbols**

Run: `go test ./internal/registry -run TestNormalizeDomain -v`

Expected: FAIL with undefined `NormalizeDomain`.

- [ ] **Step 3: Implement typed evidence and domain normalization**

Use exact enums:

```go
type TrustState string
const (
    TrustCandidate TrustState = "candidate"
    TrustTrusted   TrustState = "trusted"
    TrustBlocked   TrustState = "blocked"
)

type EvidenceKind string
const (
    EvidenceWhitelist EvidenceKind = "historical_whitelist"
    EvidenceMention   EvidenceKind = "mention"
    EvidenceAlias     EvidenceKind = "alias"
    EvidenceWarning   EvidenceKind = "warning"
)
```

`Shop` contains `NetworkDomain`, `DisplayDomain`, `TrustState`, and `Enabled`. `Evidence` contains `NetworkDomain`, `DisplayDomain`, optional `RelatedNetworkDomain` and `RelatedDisplayDomain` for aliases, `Kind`, `Page`, `PostURL`, `ObservedAt`, `Excerpt`, and a SHA-256 idempotency key over those source fields.

- [ ] **Step 4: Run registry domain tests**

Run: `gofmt -w internal/registry && go test ./internal/registry -v`

Expected: PASS.

- [ ] **Step 5: Commit registry types**

```bash
git add go.mod go.sum internal/registry
git commit -m "feat: add shop registry evidence model"
```

### Task 2: Rendered Fragrantica Capture Worker

**Files:**
- Create: `internal/capture/fragrantica.go`
- Test: `internal/capture/fragrantica_test.go`
- Create: `cmd/fragranticacapture/main.go`
- Create: `Dockerfile.capture`
- Modify: `.gitignore`

**Interfaces:**
- Produces: `capture.Fragrantica.Capture(ctx, topicURL, pageCount, outputDir) error` and files `page-01.html` through `page-16.html`.

- [ ] **Step 1: Add chromedp and write failing navigation/access-control tests**

Run: `go get github.com/chromedp/chromedp`

Use an `httptest.Server` plus injected `Navigator` to assert exact page URLs, deterministic filenames, and fail-closed behavior:

```go
func TestPageURL(t *testing.T) {
    if got := PageURL("https://www.fragrantica.ru/board/viewtopic.php?id=235155", 1); got != "https://www.fragrantica.ru/board/viewtopic.php?id=235155" {
        t.Fatal(got)
    }
    if got := PageURL("https://www.fragrantica.ru/board/viewtopic.php?id=235155", 16); got != "https://www.fragrantica.ru/board/viewtopic.php?id=235155&p=16" {
        t.Fatal(got)
    }
}

func TestRejectsChallengePage(t *testing.T) {
    err := ValidateRenderedPage("Attention Required! | Cloudflare", "Verify you are human")
    if !errors.Is(err, ErrAccessChallenge) { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 2: Run capture tests and verify missing symbols**

Run: `go test ./internal/capture -v`

Expected: FAIL with undefined capture helpers.

- [ ] **Step 3: Implement sequential Chromium capture**

For each page, navigate with chromedp, wait for `main`, read `document.documentElement.outerHTML`, validate the title/body, and atomically write `page-%02d.html`. Stop immediately on CAPTCHA, challenge, login wall, or missing `Сообщения с` marker. Do not click or solve access controls.

- [ ] **Step 4: Add the optional capture image and run tests**

`Dockerfile.capture` installs Chromium, builds only `cmd/fragranticacapture`, mounts `/output`, and keeps this runtime separate from the bot image. Add `data/fragrantica/` to `.gitignore`.

Run: `gofmt -w cmd/fragranticacapture internal/capture && go test -race ./internal/capture -v`

Expected: PASS.

- [ ] **Step 5: Commit the capture worker**

```bash
git add go.mod go.sum cmd/fragranticacapture internal/capture Dockerfile.capture .gitignore
git commit -m "feat: capture rendered Fragrantica topic pages"
```

### Task 3: Fragrantica Page Parser and Snapshot Importer

**Files:**
- Create: `internal/registry/parser.go`
- Create: `internal/registry/importer.go`
- Create: `internal/registry/testdata/page-01.html`
- Create: `internal/registry/testdata/page-16.html`
- Test: `internal/registry/parser_test.go`
- Test: `internal/registry/importer_test.go`

**Interfaces:**
- Produces: `registry.ParsePage(page int, baseURL string, io.Reader) ([]Evidence, error)` and `registry.Importer.ImportDir(ctx, inputDir, pageCount) ([]Evidence, error)`.

- [ ] **Step 1: Capture all pages and copy representative fixtures**

```bash
go run ./cmd/fragranticacapture --topic-url 'https://www.fragrantica.ru/board/viewtopic.php?id=235155' --pages 16 --output data/fragrantica/topic-235155
mkdir -p internal/registry/testdata
cp data/fragrantica/topic-235155/page-01.html internal/registry/testdata/page-01.html
cp data/fragrantica/topic-235155/page-16.html internal/registry/testdata/page-16.html
```

If the capture reports an access challenge, stop and ask the user to provide browser-exported page HTML; do not add evasion flags.

- [ ] **Step 2: Write failing parser tests**

The page-1 test must assert historical-whitelist evidence for `randewoo.ru`, `allureparfum.ru`, `orental.ru`, `духи.рф`, and `aroma-butik.ru`. The page-16 test must assert warning evidence for `artparfum.ru` plus mention/alias evidence for `parfumday.ru` and `montale-mancera.ru`.

Run: `go test ./internal/registry -run 'TestParsePage' -v`

Expected: FAIL with undefined `ParsePage`.

- [ ] **Step 3: Implement opening-post and discussion parsing**

On page 1, locate the post containing `Адреса проверенных магазинов:` and mark only domains inside that opening post as `EvidenceWhitelist`; the rest of page 1 remains discussion. On all pages, create mention evidence for normalized external domains.

Create warning evidence only when a domain appears in the same post as one of these normalized phrases: `не отгружает`, `принимает деньги`, `поддел`, `мошенн`, `не прислали товар`, `не связываться`. Keep at most 500 Unicode characters around the domain and save the exact post permalink when present.

Create alias evidence from explicit phrases `бывший`, `новый адрес`, `называется`, `другие имена`, and `переехал`; never infer an alias from two domains merely appearing in the same post.

- [ ] **Step 4: Write and pass a deterministic 16-file importer test**

Create 16 tiny files in `t.TempDir()`, import them in ascending filename order, and fail if any page from 1 through 16 is missing. Deduplicate by evidence idempotency key without dropping different excerpts for the same domain.

Run: `gofmt -w internal/registry && go test -race ./internal/registry -v`

Expected: PASS.

- [ ] **Step 5: Commit the offline importer**

```bash
git add internal/registry
git commit -m "feat: parse captured Fragrantica shop evidence"
```

### Task 4: Registry Persistence Without Trust Mutation

**Files:**
- Create: `internal/storage/migrations/002_registry.sql`
- Create: `internal/storage/registry.go`
- Test: `internal/storage/registry_test.go`

**Interfaces:**
- Produces: `storage.Registry.ImportEvidence`, `storage.Registry.ListShops`, `storage.Registry.EvidenceForDomain`, `storage.Registry.SetTrust`.
- Consumes: `registry.Evidence`.

- [ ] **Step 1: Write a failing idempotency and trust-preservation test**

The test must:

1. seed `randewoo.ru` as `trusted` and enabled;
2. seed `artparfum.ru` as `blocked` and disabled;
3. import duplicate evidence twice;
4. assert one evidence row per idempotency key;
5. assert trust and enabled flags remain unchanged.

- [ ] **Step 2: Run the storage test and verify missing migration/repository**

Run: `go test ./internal/storage -run TestRegistryImportPreservesTrust -v`

Expected: FAIL because registry tables and methods do not exist.

- [ ] **Step 3: Implement schema and transaction**

Add tables `shop_evidence` and `shop_aliases`. Use `INSERT ... ON CONFLICT(idempotency_key) DO NOTHING` for evidence. When a new domain appears, insert a `shops` row with `trust_state='candidate'` and `enabled=0`; on conflict, update only display metadata and `updated_at`, never trust or enabled state.

`SetTrust` is the sole method allowed to change `trust_state` and `enabled`, and the CLI must call it only through an explicit `set-trust` command.

- [ ] **Step 4: Run storage tests**

Run: `gofmt -w internal/storage && go test -race ./internal/storage -v`

Expected: PASS.

- [ ] **Step 5: Commit registry persistence**

```bash
git add internal/storage
git commit -m "feat: persist auditable shop evidence"
```

### Task 5: Safe Homepage Health Sweep for the Full Registry

**Files:**
- Create: `internal/registryhealth/checker.go`
- Create: `internal/registryhealth/scheduler.go`
- Test: `internal/registryhealth/checker_test.go`
- Test: `internal/registryhealth/scheduler_test.go`
- Modify: `cmd/perfumebot/main.go`

**Interfaces:**
- Produces: `registryhealth.Checker.Check(ctx, registry.Shop) shop.HealthResult`, `registryhealth.Scheduler.Run(ctx)`, and `probe_kind=homepage` health records.
- Consumes: registry shop listing and `storage.Health.Record`.

- [ ] **Step 1: Write failing SSRF, redirect, and status tests**

Use injected DNS resolution and HTTP transport. Assert that loopback, RFC1918, link-local, multicast, and IPv6 unique-local addresses are rejected before connecting; validate every redirect target with the same rule.

Use `httptest.Server` cases to assert:

- `200` with ordinary HTML → `healthy`;
- public-host redirect to a new canonical host → `redirected`;
- `403`, `429`, or a challenge page → `degraded`;
- DNS error or timeout → failed probe, persisted as `down` only on the third consecutive failure.

- [ ] **Step 2: Run health tests and verify missing checker**

Run: `go test ./internal/registryhealth -v`

Expected: FAIL with undefined checker and scheduler.

- [ ] **Step 3: Implement bounded public-network probes**

Resolve the network domain, reject non-public addresses, then perform a GET with an 8-second context timeout, `Range: bytes=0-65535`, at most 5 validated redirects, and a 64 KiB response cap. Do not retry `403` or `429`, submit forms, accept cookies, execute JavaScript, or solve challenges.

Run no more than 10 homepage probes concurrently. Record results with `probe_kind=homepage`; this never overwrites the five adapters' `probe_kind=search` status.

- [ ] **Step 4: Start the sweep with the bot and verify scheduling**

The scheduler performs one sweep at startup and repeats every 6 hours. Add an integration test with a fake registry and ticker that asserts all enabled and disabled imported shops are probed once per tick and shutdown cancels in-flight probes.

Run: `gofmt -w internal/registryhealth cmd/perfumebot && go test -race ./internal/registryhealth ./cmd/perfumebot -v`

Expected: PASS.

- [ ] **Step 5: Commit full-registry health checks**

```bash
git add internal/registryhealth cmd/perfumebot
git commit -m "feat: monitor all imported shop domains"
```

### Task 6: Registry CLI and End-to-End Import Verification

**Files:**
- Create: `cmd/shopregistry/main.go`
- Create: `internal/registrycli/app.go`
- Test: `internal/registrycli/app_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces commands `import`, `list`, `evidence`, and `set-trust`.
- Consumes: registry importer and storage repository.

- [ ] **Step 1: Write failing CLI tests**

Use injected stdout and fake importer. Assert:

- `import --input-dir data/fragrantica/topic-235155 --pages 16` imports and prints counts by evidence kind;
- `list` prints network domain, display domain, trust, enabled, and current health;
- `evidence --domain artparfum.ru` prints source URLs and excerpts;
- `set-trust --domain randewoo.ru --trust trusted --enabled=true` changes only that domain;
- invalid trust values return exit code 2.

- [ ] **Step 2: Run CLI tests and verify failure**

Run: `go test ./internal/registrycli -v`

Expected: FAIL with missing CLI app.

- [ ] **Step 3: Implement explicit commands and seed approved stores**

At database initialization, seed the five approved stores as `trusted` and enabled. Seed all other imported domains as candidate and disabled. `import` performs no trust changes.

Document exact commands:

```bash
go run ./cmd/fragranticacapture --topic-url 'https://www.fragrantica.ru/board/viewtopic.php?id=235155' --pages 16 --output data/fragrantica/topic-235155
go run ./cmd/shopregistry import --input-dir data/fragrantica/topic-235155 --pages 16
go run ./cmd/shopregistry list
go run ./cmd/shopregistry evidence --domain artparfum.ru
```

- [ ] **Step 4: Run full verification and a live import**

Run:

```bash
go test -race ./internal/registry ./internal/storage ./internal/registrycli
go vet ./...
go run ./cmd/fragranticacapture --topic-url 'https://www.fragrantica.ru/board/viewtopic.php?id=235155' --pages 16 --output data/fragrantica/topic-235155
go run ./cmd/shopregistry import --input-dir data/fragrantica/topic-235155 --pages 16
go run ./cmd/shopregistry list
```

Expected: tests PASS; capture writes 16 rendered pages; offline import preserves the five enabled stores and creates disabled candidate/evidence records for later domains.

- [ ] **Step 5: Commit registry CLI**

```bash
git add cmd/shopregistry internal/registrycli README.md
git commit -m "feat: add Fragrantica registry CLI"
```
