# Fragrantica Enrichment and Concentration UX — Design

## Goal

Make the Telegram selection flow less confusing by offering only concentrations observed for the selected fragrance. Enrich the final price result with a best-effort Fragrantica visual containing the bottle and its main accords.

Price comparison remains the primary function. Fragrantica failure must never delay or suppress store results.

## User Experience

The fragrance discovery result is grouped by normalized brand, name, and edition. Every candidate also carries the distinct recognized concentrations observed in its store offers.

When the user has not supplied a concentration:

- one observed concentration is selected automatically;
- two or more observed concentrations are shown as inline buttons in canonical order;
- zero observed concentrations produce a concise request to repeat the query with an explicit concentration, for example `Dior Sauvage EDP`; the bot never shows a guessed fixed list.

An explicit concentration from the user's query always wins. Volume and product-kind selection keep their existing behavior.

The bot sends the price result first. It then attempts Fragrantica enrichment for the exact normalized brand, fragrance, edition, and concentration. On success it sends one additional Telegram photo:

- a cropped visual with the bottle and main accord bars;
- a short caption naming the fragrance and linking to its canonical Fragrantica page.

If the enrichment is unavailable, ambiguous, challenged, timed out, or stale because a newer user search started, the bot sends no extra error message. The already delivered price result remains valid.

## Architecture

Docker Compose gains a private `fragrantica-enricher` service built from a dedicated Chromium image. The main bot remains a small distroless non-root container.

The enricher exposes only an internal HTTP API:

```text
POST /v1/enrich
GET  /healthz
```

`POST /v1/enrich` accepts a normalized fragrance identity and returns either:

- `200 application/json` with `source_url`, `title`, `accords`, and `png_base64` containing a decoded PNG no larger than 5 MB;
- `404` for no unambiguous match;
- `429` when the single browser worker is busy;
- `503` for an access challenge or unavailable browser;
- `504` for a timeout.

No enricher port is published on the VPS. The service accepts requests only from the Compose network.

## Fragrantica Browser Flow

The sidecar reuses one Chromium process and permits one lookup at a time. A lookup has a hard 15-second deadline.

The browser:

1. opens Fragrantica search with the normalized brand, name, edition, and concentration;
2. collects perfume candidates from the rendered result page;
3. normalizes each candidate by removing concentration aliases from its displayed fragrance name;
4. requires an exact normalized brand, base fragrance name, supplied edition, and parsed concentration match;
5. rejects multiple exact matches instead of guessing;
6. opens the selected perfume page;
7. extracts the canonical URL, bottle image, and main accord labels/bars;
8. screenshots only the bottle-and-accord card, never the complete page.

Selectors use semantic text and attributes with narrowly scoped fallbacks. Captured HTML fixtures protect the parser against markup drift. The implementation detects Cloudflare, CAPTCHA, and human-verification pages and returns `503`; it does not solve or bypass them.

The PNG response is limited to 5 MB and validated before being passed to Telegram. Fragrantica and its static image hosts are the only permitted navigation and asset hosts. Redirects and DNS resolution are revalidated to preserve the project's SSRF protections.

## Bot Integration

The bot receives an optional `Enricher` dependency. If `FRAGRANTICA_ENRICHER_URL` is empty, price search works normally without visual enrichment.

Candidate session state becomes explicit:

```go
type FragranceCandidate struct {
    Query          SearchQuery
    Concentrations []Concentration
}
```

Candidates and their concentrations remain in the existing JSON session payload, so no session-table migration is required.

The final interaction sequence is:

1. complete exact store search;
2. send rendered price results immediately;
3. start bounded enrichment under the same per-user cancellation ownership;
4. verify the search is still current;
5. upload the PNG with a caption.

The active-search mutex must never be held during SQLite, Telegram, browser, or HTTP I/O. Ownership checks are short critical sections. Starting a newer search cancels the previous price search and any pending enrichment so a stale Fragrantica card cannot appear after a newer query.

## Cache

Successful enrichment is cached in the bot's SQLite database for seven days using a normalized key over brand, name, edition, and concentration. The cache stores:

- canonical Fragrantica URL;
- accord labels;
- PNG bytes;
- retrieval timestamp.

Negative results and challenge responses are not persisted. The cache has a 5 MB per-entry limit and a 256 MB total limit. After a successful write crosses the total limit, the oldest entries are removed until the cache is at or below 192 MB. A new `003_fragrantica_enrichment.sql` migration creates the cache table without modifying existing price, registry, or session data.

## Failure Handling

- Price results are sent before enrichment begins.
- Sidecar absence, timeout, challenge, parse failure, image failure, and Telegram photo failure are logged with sanitized metadata and do not produce a second user-visible failure.
- A weak or tied Fragrantica match is treated as no result.
- A canceled or superseded Telegram session cannot send a visual.
- The sidecar healthcheck reports browser readiness, not merely HTTP listener readiness.
- Full Fragrantica pages, cookies, Telegram updates, and image bytes are not logged.

## Deployment

`docker-compose.yml` adds the sidecar, an internal healthcheck, and `FRAGRANTICA_ENRICHER_URL=http://fragrantica-enricher:8081` for the bot. Chromium runs as a non-root user with a read-only root filesystem, a writable temporary directory, bounded shared memory, dropped capabilities, and no published ports.

The price bot may start and remain healthy while the sidecar is unavailable. The sidecar is optional degradation, not a hard startup dependency.

Expected additional steady-state cost is one idle Chromium process. Compose limits the sidecar to 512 MB of memory, gives Chromium 128 MB of shared memory, and limits browser concurrency to one.

## Testing

Implementation follows test-first development and includes:

- candidate aggregation tests proving only observed concentrations are retained;
- Telegram-flow tests for zero, one, multiple, explicit, canceled, and superseded concentrations;
- Fragrantica candidate-scoring tests including editions and similarly named flankers;
- captured search/product fixtures for bottle and accord extraction;
- challenge, ambiguous-result, timeout, oversized-image, invalid-content-type, redirect, and SSRF tests;
- Telegram transport tests for PNG upload and sanitized caption;
- SQLite enrichment-cache round-trip, TTL, size-limit, and migration tests;
- sidecar API contract tests;
- Docker Compose validation and an opt-in Chromium smoke test.

Live Fragrantica access is never required by the default unit-test suite.

## Acceptance Criteria

1. The bot never displays a concentration that was not present in discovery offers unless the user explicitly typed it.
2. A single observed concentration is selected without another question.
3. Multiple observed concentrations are the only concentration buttons shown.
4. No observed concentration yields an actionable retry prompt rather than a guessed list.
5. Price results are sent even when the sidecar is stopped or challenged.
6. A successful exact Fragrantica match produces one bottle-and-accord PNG with a canonical link.
7. Ambiguous, stale, challenged, or timed-out enrichment produces no misleading card.
8. Existing SQLite data survives the new migration and the Docker deployment remains non-root.
