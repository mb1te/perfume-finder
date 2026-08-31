# Perfume Price Bot — MVP Design

## Goal

Build a Telegram bot that finds the cheapest matching perfume offer among five approved Russian stores. The bot must compare the same product variant rather than ranking a sample against a retail bottle.

The MVP runs in Docker on a Yandex Cloud VPS and reports product prices in rubles without delivery costs.

## Scope

Enabled stores:

1. `randewoo.ru`
2. `allureparfum.ru`
3. `orental.ru`
4. `духи.рф` (`xn--d1ai6ai.xn--p1ai`)
5. `aroma-butik.ru`

The system also imports the complete 16-page Fragrantica thread `235155` as source material. The opening post supplies the historical whitelist; later messages supply candidates, aliases, moves, and warnings. A forum mention never promotes a store automatically.

Out of scope for the MVP:

- delivery price calculation;
- purchases, carts, or checkout automation;
- price alerts and subscriptions;
- stores outside the approved five;
- automatic trust decisions based on forum comments;
- a web administration interface;
- multi-instance deployment.

## User Experience

The user sends a free-text query such as `Dior Sauvage` or a complete query such as `Dior Sauvage EDT 100 мл`.

The bot resolves missing dimensions with inline buttons:

1. fragrance and edition;
2. concentration;
3. volume;
4. product kind.

Supported product kinds:

- retail bottle;
- tester;
- decant;
- miniature;
- sample;
- all kinds.

When the user selects all kinds, results are grouped by kind. Prices from different kinds are never ranked in one list.

The final response contains:

- normalized product variant;
- offers sorted by product price;
- store name;
- availability;
- direct product link;
- check time;
- a concise warning for stores that timed out or failed.

One failed store does not block results from other stores. If no exact variant exists, the bot says so and may show separately labeled nearby variants; it does not silently mix them into the exact result.

## Architecture

The primary service is written in Go and runs as one process for the MVP.

Main components:

- **Telegram transport** handles long polling, commands, messages, and inline-button state.
- **Query normalizer** converts user text and button selections into a canonical `SearchQuery`.
- **Search coordinator** executes enabled store adapters concurrently with bounded timeouts.
- **Store adapters** translate a canonical query into a store search and normalize returned offers.
- **Variant matcher** classifies product variants and rejects non-equivalent results.
- **Ranker** groups offers by kind and sorts each group by price.
- **SQLite repository** stores short-lived search cache, shop registry, aliases, and health history.
- **Health scheduler** probes store homepages and search paths every six hours.
- **Fragrantica importer** updates source evidence for the registry through an explicit CLI command.

The bot uses Telegram long polling. A webhook, public TLS endpoint, PostgreSQL, and distributed queue are unnecessary for a single VPS MVP.

## Store Adapter Contract

Each adapter implements the same behavior:

```go
type ShopAdapter interface {
    ID() ShopID
    Search(ctx context.Context, query SearchQuery) ([]Offer, error)
    Health(ctx context.Context) HealthResult
}
```

HTTP and public AJAX endpoints are preferred. HTML pages are parsed with stable semantic selectors and store-specific fallback selectors.

Observed search surfaces at design time:

- Allure Parfum, Orental, and Aroma Butik expose ordinary HTML search results.
- Randewoo and Духи.рф use client-side or AJAX-assisted search and need endpoint discovery.

The implementation must first identify the HTTP/AJAX contracts used by Randewoo and Духи.рф. A browser worker is an optional last resort and remains outside the primary Go container unless a direct request cannot reproduce the search reliably. The code must not bypass CAPTCHAs or other access controls.

Every adapter returns partial successes independently. A per-store timeout defaults to eight seconds. Global and per-store concurrency limits prevent the bot from hammering shop sites.

## Domain Model

`SearchQuery` contains:

- normalized brand;
- fragrance name;
- edition or year when applicable;
- concentration;
- volume in milliliters;
- selected product kind.

`Offer` contains:

- store ID;
- raw title;
- normalized brand and fragrance;
- edition or year;
- concentration;
- volume in milliliters;
- product kind;
- integer price in kopecks;
- stock status;
- canonical product URL;
- retrieval timestamp.

Concentration aliases include forms such as `EDT` / `Eau de Toilette` and `EDP` / `Eau de Parfum`. Product-kind classification uses explicit store labels and conservative title rules. An unknown kind is never treated as a retail bottle.

## Matching Rules

Fuzzy matching is allowed only while discovering candidate fragrances. Final offer comparison requires equality after normalization for:

- brand;
- fragrance and edition;
- concentration;
- volume;
- product kind, unless the user selected all kinds.

The matcher must distinguish similarly named products such as `Sauvage`, `Eau Sauvage`, `Sauvage Elixir`, and `Sauvage Eau Forte`.

When a store only exposes a price range in search results, the adapter opens the product page and extracts the price of the selected volume and kind. A range minimum is never reported as the price of a 100 ml bottle without verifying that variant.

## Fragrantica Import and Shop Registry

An explicit CLI command parses all pages of topic `235155` and records:

- domains from the opening whitelist;
- first and latest mentions;
- candidate domains introduced later;
- aliases and redirects;
- warning excerpts and their source links.

Registry trust states are `trusted`, `candidate`, and `blocked`. Imports append evidence but do not change trust state or enable a store automatically.

The five MVP stores are enabled explicitly in configuration. Internationalized domains are stored in Unicode for display and Punycode for networking.

## Health Checks

Every six hours, the scheduler performs a lightweight homepage probe and a known search probe. Status values are:

- `healthy` — the site and search result parser work;
- `redirected` — the configured domain redirects to a canonical domain;
- `degraded` — timeout, anti-bot response, or changed markup;
- `down` — three consecutive failed checks;
- `disabled` — administratively excluded.

The bot still attempts an enabled degraded store during a user search. It reports real-time failures in the response rather than hiding the store silently.

## Cache and Load Control

Search results are cached in SQLite for 15 minutes by normalized query. Expired prices are not shown as current results.

The service applies:

- a global search concurrency limit;
- a per-store concurrency limit;
- a per-user Telegram rate limit;
- request cancellation when the Telegram interaction is abandoned or superseded.

These controls, plus caching, keep traffic low and make the system suitable for one small VPS.

## Failure Handling

Adapter failures are isolated and represented as typed errors: timeout, access denied, anti-bot challenge, parse failure, and no results.

Rules:

- `no results` is not a site failure;
- one failed adapter never cancels successful adapters;
- malformed or ambiguous prices are discarded and logged;
- changed markup marks health as degraded and creates a clear log event;
- no cached value older than the 15-minute TTL is presented as live;
- Telegram responses escape store-provided text and only use validated `http` or `https` links.

## Persistence

SQLite is sufficient for the single-container MVP. The database lives on a Docker volume and stores:

- shop registry and evidence;
- aliases;
- health checks;
- search cache;
- minimal conversational state needed for inline-button flows.

Schema migrations run explicitly during container startup. PostgreSQL is deferred until multi-instance deployment is required.

## Deployment and Security

Docker Compose runs the Go bot with a persistent data volume and restart policy. The default image contains only the compiled Go service and required certificates.

`TELEGRAM_BOT_TOKEN` and any future credentials are injected through environment variables. `.env` is ignored by Git. Logs must not contain tokens, cookies, full Telegram updates, or other sensitive payloads.

The service exposes a local health endpoint for Docker and VPS monitoring. Structured logs include adapter ID, duration, outcome, and correlation ID.

## Testing

The test suite includes:

- parser fixtures captured from each store's search and product pages;
- adapter contract tests;
- a matching matrix covering concentration, volume, kind, edition, and confusing names;
- Telegram handler and callback-state tests;
- cache, timeout, and partial-failure tests;
- Fragrantica importer fixtures for whitelist entries, candidates, aliases, and warnings;
- opt-in live smoke tests that do not run in the default unit-test suite;
- Docker startup and health-check smoke tests.

Fixture tests are the primary protection against markup changes. Live smoke tests verify current integration without making normal CI depend on external sites.

## Acceptance Criteria

The MVP is complete when:

1. A user can search for a fragrance and resolve edition, concentration, volume, and kind through Telegram.
2. All five enabled adapters return verified exact-variant prices for representative fixtures and live smoke queries.
3. Results are grouped by product kind and sorted by product price without delivery.
4. A sample, miniature, decant, tester, or another volume cannot win against a requested retail bottle.
5. Failure of any one store produces a partial result with a warning within the overall timeout.
6. The full Fragrantica topic is importable without automatically trusting new domains.
7. Store health is persisted and refreshed every six hours.
8. The bot runs from Docker Compose on a single Yandex Cloud VPS with secrets supplied only at runtime.

