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

Modern frontend frameworks like **Next.js** already embed their entire data layer as structured JSON directly in the HTML source — hidden in plain sight inside `<script id="__NEXT_DATA__">` tags. No JavaScript execution required.

**Nobody is extracting it efficiently.**

## The Solution

SwipeNode is a single, statically-compiled binary that fetches raw HTML and surgically extracts the structured data that frameworks embed at build time.

```
┌──────────────┐     HTTP GET      ┌──────────────┐     CSS Selector     ┌──────────────┐
│              │  ──────────────►  │              │  ──────────────────►  │              │
│   AI Agent   │                   │  Raw HTML    │                       │  Clean JSON  │
│              │  ◄──────────────  │  (no render) │  ◄──────────────────  │  to stdout   │
└──────────────┘   structured data └──────────────┘    __NEXT_DATA__      └──────────────┘
```

No browser. No JavaScript engine. No render pipeline. Just the data.

## Features

- **Instant extraction** — Single HTTP request, CSS selector match, done. Milliseconds, not seconds.
- **Zero dependencies at runtime** — Ships as one static binary. No Node.js, no Chrome, no container images.
- **AI-agent friendly** — Clean JSON to stdout, errors to stderr. Pipe it, parse it, chain it.
- **Realistic HTTP fingerprint** — Proper `User-Agent` and `Accept` headers to avoid bot detection.
- **Framework-aware** — Purpose-built selectors that understand how frameworks embed data, starting with Next.js.

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
# Extract Next.js hydration data from any Next.js-powered page
swipenode extract --url "https://example.com/page"

# Pipe to jq for pretty-printed, queryable JSON
swipenode extract --url "https://example.com/page" | jq '.props.pageProps'

# Use in an AI agent pipeline
DATA=$(swipenode extract --url "$TARGET_URL" 2>/dev/null)
```

### CLI Reference

```
swipenode
├── extract          Extract structured data from a URL
│   └── --url        Target URL to extract __NEXT_DATA__ from
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
        └── nextjs.go           # Next.js __NEXT_DATA__ extractor
```

The design follows three principles:

1. **Separation of concerns** — The `pkg/extractor` package is a pure library with no CLI dependencies. Import it in your own Go code, call `extractor.ExtractNextData(url)`, get JSON back.

2. **Stdout is sacred** — Only clean, parseable JSON hits stdout. All errors, warnings, and diagnostics go to stderr. This makes SwipeNode a reliable component in shell pipelines and agent tool chains.

3. **One job, done well** — Each extractor targets a specific framework's data embedding pattern with a precise CSS selector, not a generic scraper that returns noisy HTML.

## How It Works

Most Next.js applications embed their complete page data in a script tag during server-side rendering:

```html
<script id="__NEXT_DATA__" type="application/json">
  {"props":{"pageProps":{"title":"...","data":[...]}},"page":"/","query":{}}
</script>
```

SwipeNode's extraction pipeline:

1. **Fetch** — HTTP GET with a realistic browser `User-Agent` and `Accept` header
2. **Parse** — Stream the HTML response through goquery (built on Go's `net/html`)
3. **Select** — Target `script#__NEXT_DATA__[type="application/json"]` with a single CSS selector
4. **Return** — Trim whitespace, validate non-empty, print raw JSON to stdout

The entire operation is a single HTTP round-trip with in-memory HTML parsing. No DOM construction, no layout calculation, no paint cycle.

## Roadmap

SwipeNode is starting with Next.js, but the architecture is designed to grow:

- [ ] **Next.js** `__NEXT_DATA__` extraction — *shipped*
- [ ] **Nuxt.js** `__NUXT_DATA__` extraction
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

To add a new extractor:

1. Create a new file in `pkg/extractor/` (e.g., `nuxt.go`)
2. Implement a function following the same pattern as `ExtractNextData`
3. Add a new subcommand in `cmd/swipenode/` that calls your extractor
4. Submit a PR

## License

MIT

---

<p align="center">
  <sub>Built for the age of AI agents. No browsers were harmed in the making of this tool.</sub>
</p>
