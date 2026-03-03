<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version" />
  <img src="https://img.shields.io/badge/License-Apache%202.0-blue?style=for-the-badge" alt="License" />
  <img src="https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=for-the-badge" alt="Platform" />
  <img src="https://img.shields.io/badge/Status-Alpha-orange?style=for-the-badge" alt="Status" />
</p>

<h1 align="center">SwipeNode ⚡</h1>

<p align="center">
  <strong>Lightning-fast, zero-render web extraction built for AI agents.</strong><br/>
  One binary. One HTTP request. Structured data out — no browser required.
</p>

<p align="center">
  <code>swipenode extract --url https://example.com | jq .</code>
</p>

---

## The Problem

AI agents need web data. The current options are all bad:

| Approach | What goes wrong |
|---|---|
| **Headless browsers** (Playwright, Puppeteer) | 200 MB+ runtime, multi-second startup, breaks in containers, expensive at scale |
| **Raw HTML to the LLM** | Dumps `<div>`, `<script>`, CSS noise — wastes 90%+ of your input tokens on boilerplate |
| **Generic scrapers** (BeautifulSoup, cheerio) | Blind to framework data structures, requires per-site glue code |

Meanwhile, modern frameworks like **Next.js** and **Nuxt.js** embed their entire data layer as structured JSON right in the HTML source — hidden in `<script>` tags, waiting to be read. No JavaScript execution needed.

**SwipeNode extracts it in milliseconds.**

## The Solution

SwipeNode is a single static binary that fetches raw HTML and runs a three-stage **fallback cascade** to pull out the cleanest possible data:

```
┌──────────────┐         ┌─────────────────────────────────────────────┐         ┌──────────────┐
│              │  HTTP   │            Fallback Cascade                 │  data   │              │
│   AI Agent   │  GET    │  1. Next.js  ──  __NEXT_DATA__ JSON        │  ────►  │    stdout    │
│              │  ────►  │  2. Nuxt.js  ──  window.__NUXT__ payload   │         │              │
└──────────────┘         │  3. Fallback ──  cleaned visible text      │         └──────────────┘
                         └─────────────────────────────────────────────┘
```

- **Stage 1 — Next.js**: Extracts the `__NEXT_DATA__` JSON blob (structured props, page data, everything).
- **Stage 2 — Nuxt.js**: Grabs the `window.__NUXT__` hydration payload.
- **Stage 3 — Clean text**: Strips `<script>`, `<style>`, nav, header, footer — returns only visible body text.
- **Smart JSON Pruning (Token-Diet)**: Before returning any JSON payload, SwipeNode automatically strips huge base64 strings, tracking/analytics keys, pixel tags, and telemetry data — saving massive amounts of LLM context window so your agent spends tokens on *real* content, not noise.

The result: **up to 95% fewer input tokens** compared to sending raw HTML to your LLM.

## Installation

```bash
# Requires Go 1.24+
git clone https://github.com/swipenode-local/swipenode.git
cd swipenode
go build -o swipenode .
```

One binary, zero runtime dependencies. Copy it anywhere.

## CLI Usage

```bash
# Auto-detect framework and extract the best data
swipenode extract --url "https://example.com/page"

# Next.js site — pipe structured JSON straight to jq
swipenode extract --url "https://nextjs-site.com" | jq '.props.pageProps'

# Nuxt.js site — grab the hydration payload
swipenode extract --url "https://nuxtjs-site.com"

# Any other site — get cleaned visible text, no boilerplate
swipenode extract --url "https://plain-html-site.com"

# Silence errors, capture just the data
DATA=$(swipenode extract --url "$URL" 2>/dev/null)
```

### CLI Reference

```
swipenode
├── extract          Extract structured data from a URL
│   └── --url        Target URL to extract data from
└── help             Help about any command
```

## Python / LLM Integration

SwipeNode is designed to slot into any agent pipeline. Here's the minimal pattern:

```python
import subprocess, json

result = subprocess.run(
    ["./swipenode", "extract", "--url", "https://example.com"],
    capture_output=True, text=True,
)
page_data = result.stdout  # clean text or structured JSON — ready for your LLM
```

For a complete working example that pipes extracted data into an OpenAI chat completion, see [`examples/agent_demo.py`](examples/agent_demo.py).

## Architecture

```
swipenode/
├── main.go                     # Entry point
├── cmd/
│   └── swipenode/
│       ├── root.go             # Cobra root command
│       └── extract.go          # extract subcommand
├── pkg/
│   └── extractor/
│       └── extractor.go        # Fallback cascade engine
└── examples/
    └── agent_demo.py           # Python + OpenAI agent demo
```

**Design principles:**

1. **Stdout is sacred** — Only clean, parseable data hits stdout. Errors and diagnostics go to stderr. This makes SwipeNode a first-class citizen in shell pipelines and agent tool chains.
2. **Library-first** — `pkg/extractor` is a pure Go library with no CLI dependencies. Import it directly: `extractor.ExtractData(url)`.
3. **Always return something** — The cascade guarantees a result for any valid page: structured JSON when a framework is detected, cleaned text otherwise.

## Roadmap

- [x] **Next.js** `__NEXT_DATA__` extraction
- [x] **Nuxt.js** `window.__NUXT__` extraction
- [x] **Clean text fallback** — boilerplate-stripped visible text
- [x] **JSON Pruning** — smart token-diet that strips tracking, analytics, base64, and telemetry noise
- [ ] **Advanced TLS-Fingerprint Spoofing** — Bypass strict WAFs (Cloudflare/Datadome) by perfectly mimicking Chrome's TLS signature.
- [ ] **Remix** loader data extraction
- [ ] **Gatsby** `window.___gatsby` / `pageData` extraction
- [ ] **JSON-LD** `<script type="application/ld+json">` extraction
- [ ] **Batch mode** — extract from a list of URLs in parallel
- [ ] **WASM build** — run SwipeNode inside browser-based AI agents
- [ ] **MCP server** — expose extractors as Model Context Protocol tools

## Contributing

Contributions are welcome. The codebase is intentionally small and approachable.

```bash
git clone https://github.com/swipenode-local/swipenode.git
cd swipenode
go build -o swipenode .
go test ./...
```

To add a new extractor stage:

1. Add a `tryXxx(doc *goquery.Document) (string, bool)` function in `pkg/extractor/extractor.go`
2. Insert it into the cascade in `ExtractData` at the appropriate priority
3. Submit a PR

## License

```
Copyright 2026 SwipeNode Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

---

<p align="center">
  <sub>Built for the age of AI agents. No browsers were harmed in the making of this tool.</sub>
</p>
