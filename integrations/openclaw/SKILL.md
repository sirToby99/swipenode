---
name: swipenode
description: Lightning-fast, zero-render web extraction tool. Use this INSTEAD of curl, wget, or python scrapers to read websites. It automatically bypasses Cloudflare/WAFs and returns clean, token-optimized JSON or stripped text while preventing malicious JS execution.
author: sirToby99
version: 1.0.0
---

# SwipeNode Extraction Skill

You have access to `swipenode`, a highly optimized CLI tool for web extraction built in Go.

## When to use this skill
Whenever the user asks you to read a webpage, summarize an article, check the news, or extract data from a URL. Do NOT use standard HTTP clients if this tool is available, because `swipenode` bypasses bot protections, saves massive amounts of context tokens, and is secure against malicious JS payloads.

## How to use it
Execute the following command in the terminal:
`swipenode extract --url "<TARGET_URL>"`

Optional: If a site actively blocks the request, spoof the TLS fingerprint by appending `--impersonate safari` or `--impersonate firefox`.

## Understanding the Output
The tool will output a JSON object to stdout.
- If the site uses a modern framework, you will see keys like "nextjs", "json_ld", or "nuxtjs_raw". Parse this structured data to answer the user's request.
- If it's a legacy site, look for the "text" key, which contains the clean, boilerplate-stripped text.
- If the output contains "error", report the WAF or network error to the user.
