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

	data, err := extractData(srv.URL, "chrome", true)
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

	data, err := extractData(srv.URL, "chrome", true)
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

	data, err := extractData(srv.URL, "chrome", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Boilerplate elements should be stripped
	if strings.Contains(data, "tracking") {
		t.Error("expected <script> content to be removed")
	}
	if strings.Contains(data, "color: red") {
		t.Error("expected <style> content to be removed")
	}
	if strings.Contains(data, "Menu Item") {
		t.Error("expected <header>/<nav> content to be removed")
	}
	if strings.Contains(data, "Copyright") {
		t.Error("expected <footer> content to be removed")
	}

	// Actual content should survive
	if !strings.Contains(data, "Welcome to My Site") {
		t.Errorf("expected main content to be preserved, got: %s", data)
	}
	if !strings.Contains(data, "main content of the page") {
		t.Errorf("expected paragraph text to be preserved, got: %s", data)
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

	data, err := extractData(srv.URL, "chrome", true)
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

	_, err := extractData(srv.URL, "chrome", true)
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

	_, _ = extractData(srv.URL, "chrome", true)

	if gotUA != expectedUA {
		t.Errorf("expected User-Agent %q, got %q", expectedUA, gotUA)
	}
}

func TestExtractDataStandardHTTPProfilesAndRedirectPolicy(t *testing.T) {
	profiles := map[string]string{
		"chrome":  "Chrome/120.0.0.0",
		"firefox": "Firefox/120.0",
		"safari":  "Safari/604.1",
	}
	for profile, marker := range profiles {
		t.Run(profile, func(t *testing.T) {
			var gotUA string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUA = r.UserAgent()
				_, _ = w.Write([]byte("<html><body>technical document</body></html>"))
			}))
			defer srv.Close()

			if _, err := extractData(srv.URL, profile, true); err != nil {
				t.Fatalf("extract with %s profile: %v", profile, err)
			}
			if !strings.Contains(gotUA, marker) {
				t.Fatalf("user-agent %q does not contain %q", gotUA, marker)
			}
		})
	}

	var redirected bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected = true
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	if _, err := extractData(source.URL, "chrome", true); err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("redirect did not fail closed: %v", err)
	}
	if redirected {
		t.Fatal("standard HTTP client followed a redirect")
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

	data, err := extractData(srv.URL, "chrome", true)
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

	data, err := extractData(srv.URL, "chrome", true)
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

func TestExtractDataCannotEnablePrivateTargetsThroughEnvironment(t *testing.T) {
	t.Setenv("SWIPENODE_TEST_MODE", "1")
	if _, err := ExtractData("http://127.0.0.1/", "chrome"); err == nil || !strings.Contains(err.Error(), "private/internal") {
		t.Fatalf("environment enabled a private target: %v", err)
	}
}

func TestValidateURLRejectsEmbeddedCredentials(t *testing.T) {
	if _, err := validateURL("https://user:password@example.com/", false); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("URL credentials were accepted: %v", err)
	}
}

func TestValidateURLRejectsSpecialUseAddresses(t *testing.T) {
	for _, target := range []string{
		"http://100.64.0.1/",
		"http://192.0.2.1/",
		"http://198.18.0.1/",
		"http://198.51.100.1/",
		"http://203.0.113.1/",
		"http://[2001:db8::1]/",
	} {
		if _, err := validateURL(target, false); err == nil || !strings.Contains(err.Error(), "private/internal") {
			t.Errorf("special-use target %s was accepted: %v", target, err)
		}
	}
}
