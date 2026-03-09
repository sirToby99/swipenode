package extractor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractData_NextJS(t *testing.T) {
	page := `<!DOCTYPE html>
<html><head><title>Next App</title></head>
<body>
<div id="__next">Hello</div>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"title":"test"}},"page":"/"}</script>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
	if !strings.Contains(data, `"nextjs"`) {
		t.Errorf("expected nextjs key in JSON output, got: %s", data)
	}
	if !strings.Contains(data, `"pageProps"`) {
		t.Errorf("expected pageProps in JSON output, got: %s", data)
	}
}

func TestExtractData_NuxtJS(t *testing.T) {
	page := `<!DOCTYPE html>
<html><head><title>Nuxt App</title></head>
<body>
<div id="__nuxt">Hello</div>
<script>window.__NUXT__={data:[{message:"hello"}],state:{count:1}}</script>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
	if !strings.Contains(data, `"nuxtjs_raw"`) {
		t.Errorf("expected nuxtjs_raw key in JSON output, got: %s", data)
	}
}

func TestExtractData_Fallback(t *testing.T) {
	page := `<!DOCTYPE html>
<html><head><title>Plain Site</title>
<style>body { color: red; }</style>
</head>
<body>
<header><nav>Menu Item 1 | Menu Item 2</nav></header>
<main>
  <h1>Welcome to My Site</h1>
  <p>This is the main content of the page.</p>
</main>
<footer>Copyright 2025</footer>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(data, "no_framework_data_found") {
		t.Errorf("expected no_framework_data_found status, got: %s", data)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
}

func TestExtractData_NextJSPriority(t *testing.T) {
	// Page has BOTH Next.js and Nuxt.js — both should appear in the output.
	page := `<!DOCTYPE html>
<html><head></head>
<body>
<script id="__NEXT_DATA__" type="application/json">{"framework":"nextjs"}</script>
<script>window.__NUXT__={framework:"nuxtjs"}</script>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
	if !strings.Contains(data, `"nextjs"`) {
		t.Errorf("expected nextjs key in output, got: %s", data)
	}
	if !strings.Contains(data, `"nuxtjs_raw"`) {
		t.Errorf("expected nuxtjs_raw key in output, got: %s", data)
	}
}

func TestExtractData_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := ExtractData(srv.URL, "chrome")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error message, got: %v", err)
	}
}

func TestExtractData_UserAgent(t *testing.T) {
	expectedUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer srv.Close()

	ExtractData(srv.URL, "chrome")

	if gotUA != expectedUA {
		t.Errorf("expected User-Agent %q, got %q", expectedUA, gotUA)
	}
}

func TestExtractData_JSONLD(t *testing.T) {
	page := `<!DOCTYPE html>
<html><head><title>Site with JSON-LD</title></head>
<body>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"Test Corp"}</script>
<p>Hello World</p>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
	if !strings.Contains(data, `"json_ld"`) {
		t.Errorf("expected json_ld key in output, got: %s", data)
	}
	if !strings.Contains(data, `"Test Corp"`) {
		t.Errorf("expected organization name in output, got: %s", data)
	}
}

func TestExtractData_Remix(t *testing.T) {
	page := `<!DOCTYPE html>
<html><head></head>
<body>
<script>window.__remixContext = {state:{loaderData:{}}}</script>
<p>Remix App</p>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	data, err := ExtractData(srv.URL, "chrome")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid([]byte(data)) {
		t.Errorf("expected valid JSON, got: %s", data)
	}
	if !strings.Contains(data, `"remix_raw"`) {
		t.Errorf("expected remix_raw key in output, got: %s", data)
	}
}
