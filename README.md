<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
  <img src="https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=for-the-badge" alt="Platform" />
  <img src="https://img.shields.io/badge/Status-Alpha-orange?style=for-the-badge" alt="Status" />
</p>

<h1 align="center">SwipeNode</h1>

<p align="center">
  <strong>Zero-render data extraction for AI agents.</strong><br/>
  Grab structured data from the web without spinning up a single browser.
</p>

<p align="center">
  <code>swipenode extract --url https://example.com</code>
</p>

---

## The Problem

AI agents need web data. Today, that means one of two painful options:

| Approach | Downsides |
|---|---|
| **Headless browsers** (Puppeteer, Playwright) | Slow startup, high memory, complex dependencies, breaks in containers |
| **Generic scrapers** (BeautifulSoup, cheerio) | No understanding of framework-specific data structures, lots of glue code |

Modern frontend frameworks like **Next.js** and **Nuxt.js** already embed their entire data layer as structured JSON directly in the HTML source — hidden in plain sight inside `<script>` tags. No JavaScript execution required.

**Nobody is extracting it efficiently.**

## The Solution

SwipeNode is a single, statically-compiled binary that fetches raw HTML and surgically extracts the structured data that frameworks embed at build time.

```
                         ┌─────────────────────────────────────────────┐
                         │            Fallback Cascade                 │
┌──────────────┐         │                                             │         ┌──────────────┐
│              │  HTTP   │  1. Next.js  ──  __NEXT_DATA__ JSON         │  data   │              │
│   AI Agent   │  GET    │  2. Nuxt.js  ──  window.__NUXT__ payload    │  ────►  │    stdout    │
│              │  ────►  │  3. Fallback ──  cleaned visible text       │         │              │
└──────────────┘         │                                             │         └──────────────┘
                         └─────────────────────────────────────────────┘
```

No browser. No JavaScript engine. No render pipeline. Just the data.

## Features

- **Instant extraction** — Single HTTP request, CSS selector match, done. Milliseconds, not seconds.
- **Zero dependencies at runtime** — Ships as one static binary. No Node.js, no Chrome, no container images.
- **AI-agent friendly** — Clean data to stdout, errors to stderr. Pipe it, parse it, chain it.
- **Realistic HTTP fingerprint** — Proper `User-Agent` and `Accept` headers to avoid bot detection.
- **Framework-aware** — Purpose-built selectors that understand how Next.js and Nuxt.js embed data.
- **Always returns something** — Fallback cascade ensures you get structured JSON, framework payloads, or cleaned visible text — never an empty result on a valid page.

## Quick Start

### Install from source

```bash
# Clone
git clone https://github.com/swipenode-local/swipenode.git
cd swipenode

# Build
go build -o swipenode .

# Run
./swipenode extract --url "https://some-nextjs-site.com"
```

### Usage

```bash
# Extract data from any page — the cascade picks the best strategy automatically
swipenode extract --url "https://example.com/page"

# Next.js site → returns raw __NEXT_DATA__ JSON, pipe to jq
swipenode extract --url "https://nextjs-site.com" | jq '.props.pageProps'

# Nuxt.js site → returns window.__NUXT__ payload
swipenode extract --url "https://nuxtjs-site.com"

# Any other site → returns cleaned visible text (boilerplate stripped)
swipenode extract --url "https://plain-html-site.com"

# Use in an AI agent pipeline
DATA=$(swipenode extract --url "$TARGET_URL" 2>/dev/null)
```

### CLI Reference

```
swipenode
├── extract          Extract structured data from a URL
│   └── --url        Target URL to extract data from
└── help             Help about any command
```

## Architecture

```
swipenode/
├── main.go                     # Entry point
├── cmd/
│   └── swipenode/
│       ├── root.go             # Cobra root command
│       └── extract.go          # extract subcommand
└── pkg/
    └── extractor/
        └── extractor.go        # Fallback cascade: Next.js → Nuxt.js → clean text
```

The design follows three principles:

1. **Separation of concerns** — The `pkg/extractor` package is a pure library with no CLI dependencies. Import it in your own Go code, call `extractor.ExtractData(url)`, get data back.

2. **Stdout is sacred** — Only clean, parseable data hits stdout. All errors, warnings, and diagnostics go to stderr. This makes SwipeNode a reliable component in shell pipelines and agent tool chains.

3. **Always return something useful** — The fallback cascade guarantees a result for any valid page: structured JSON when a framework is detected, cleaned visible text otherwise.

## How It Works

SwipeNode runs a three-stage fallback cascade against every page:

### Stage 1 — Next.js

Next.js embeds its complete page data during server-side rendering:

```html
<script id="__NEXT_DATA__" type="application/json">
  {"props":{"pageProps":{"title":"...","data":[...]}},"page":"/","query":{}}
</script>
```

SwipeNode matches `script#__NEXT_DATA__[type="application/json"]` and returns the raw JSON.

### Stage 2 — Nuxt.js

Nuxt applications hydrate via a global assignment:

```html
<script>window.__NUXT__={data:[...],state:{...}}</script>
```

SwipeNode scans all `<script>` tags for one containing `window.__NUXT__` and returns its full text.

### Stage 3 — Clean Text Fallback

If no framework payload is detected, SwipeNode strips boilerplate elements (`<script>`, `<style>`, `<noscript>`, `<header>`, `<footer>`, `<nav>`), extracts the remaining visible body text, and normalises whitespace into a clean, readable format.

### The pipeline

```
Fetch (HTTP GET) → Parse (goquery) → Cascade (Next → Nuxt → Text) → stdout
```

The entire operation is a single HTTP round-trip with in-memory HTML parsing. No DOM construction, no layout calculation, no paint cycle.

## Roadmap

SwipeNode is starting with Next.js, but the architecture is designed to grow:

- [x] **Next.js** `__NEXT_DATA__` extraction
- [x] **Nuxt.js** `window.__NUXT__` extraction
- [x] **Clean text fallback** — boilerplate-stripped visible text
- [ ] **Remix** loader data extraction
- [ ] **Gatsby** `window.___gatsby` / `pageData` extraction
- [ ] **Generic** JSON-LD / `<script type="application/ld+json">` extraction
- [ ] **Batch mode** — Extract from a list of URLs in parallel
- [ ] **WASM build** — Run SwipeNode inside browser-based AI agents
- [ ] **MCP server** — Expose extractors as Model Context Protocol tools

## Why Go?

| Reason | Detail |
|---|---|
| **Single binary** | `go build` produces one executable. No runtime, no interpreter, no `node_modules`. |
| **Cross-compilation** | Build for Linux/macOS/Windows/ARM from any machine with `GOOS` and `GOARCH`. |
| **Fast startup** | Native binary starts in milliseconds — critical for agent tool calls where latency compounds. |
| **Concurrency primitives** | Goroutines make future batch/parallel extraction trivial to implement. |

## Contributing

Contributions are welcome. The codebase is intentionally small and approachable.

```bash
# Clone and build
git clone https://github.com/swipenode-local/swipenode.git
cd swipenode
go build -o swipenode .

# Test your changes
./swipenode extract --url "https://some-nextjs-site.com"
```

To add a new extractor stage:

1. Add a `tryXxx(doc *goquery.Document) (string, bool)` function in `pkg/extractor/extractor.go`
2. Insert it into the cascade in `ExtractData` at the appropriate priority
3. Submit a PR

## License

MIT

---

<p align="center">
  <sub>Built for the age of AI agents. No browsers were harmed in the making of this tool.</sub>
</p>
