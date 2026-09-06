package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAPIReadViewsUseLocalStateAndDoNotExposeSecrets(t *testing.T) {
	service, fixture := seededService(t)
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/overview", "/api/v1/knowledge/packs", "/api/v1/evidence", "/api/v1/verifications", "/api/v1/provenance", "/api/v1/audit", "/api/v1/evidence/" + fixture.evidenceID, "/api/v1/verifications/" + fixture.verificationID, "/api/v1/audit/" + fixture.auditID} {
		response := request(handler, http.MethodGet, path, "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if strings.Contains(body, "hunter2") || strings.Contains(body, "do-not-show") || strings.Contains(body, "public_key_pem") {
			t.Fatalf("GET %s exposed a secret or signing key: %s", path, body)
		}
	}
}

func TestAPIHostRoutingCSRFAndBrowserSecurity(t *testing.T) {
	service, _ := seededService(t)
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	invalidHost := httptest.NewRequest(http.MethodGet, "http://evil.example/api/v1/overview", nil)
	invalidHost.Host = "evil.example"
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidHost)
	if invalidResponse.Code != http.StatusForbidden {
		t.Fatalf("non-loopback Host accepted: %d", invalidResponse.Code)
	}

	sessionResponse := request(handler, http.MethodGet, "/api/v1/session", "", nil)
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("session token: %s %v", sessionResponse.Body, err)
	}
	for name, headers := range map[string]map[string]string{
		"missing token": nil,
		"cross site":    {"X-SwipeNode-CSRF": session.CSRFToken, "Sec-Fetch-Site": "cross-site"},
		"wrong origin":  {"X-SwipeNode-CSRF": session.CSRFToken, "Origin": "http://evil.example"},
	} {
		response := request(handler, http.MethodPost, "/api/v1/governance/update-policy", `{"policy":"manual"}`, headers)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s mutation accepted: %d %s", name, response.Code, response.Body.String())
		}
	}
	valid := request(handler, http.MethodPost, "/api/v1/governance/update-policy", `{"policy":"auto-download"}`, map[string]string{"X-SwipeNode-CSRF": session.CSRFToken, "Origin": "http://127.0.0.1"})
	if valid.Code != http.StatusOK {
		t.Fatalf("valid local mutation rejected: %d %s", valid.Code, valid.Body.String())
	}
	query := request(handler, http.MethodGet, "/api/v1/evidence?file=/etc/passwd", "", nil)
	if query.Code != http.StatusBadRequest {
		t.Fatalf("query injection accepted: %d", query.Code)
	}
	traversal := request(handler, http.MethodGet, "/assets/../openapi.yaml", "", nil)
	if traversal.Code != http.StatusNotFound {
		t.Fatalf("asset traversal accepted: %d", traversal.Code)
	}
	index := request(handler, http.MethodGet, "/", "", nil)
	if index.Code != http.StatusOK || !strings.Contains(index.Header().Get("Content-Security-Policy"), "default-src 'self'") || strings.Contains(index.Body.String(), "<img src=x") {
		t.Fatalf("unsafe index response: %d %q", index.Code, index.Header().Get("Content-Security-Policy"))
	}
	script := request(handler, http.MethodGet, "/assets/app.js", "", nil)
	for _, sink := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(script.Body.String(), sink) {
			t.Fatalf("unsafe DOM sink %q present", sink)
		}
	}
}

func TestAPIListPaginationAndFilteringAreStrict(t *testing.T) {
	service, fixture := seededService(t)
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	response := request(handler, http.MethodGet, "/api/v1/evidence?limit=1&offset=0&q="+fixture.evidenceID, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("paged evidence request failed: %d %s", response.Code, response.Body.String())
	}
	var listing EvidenceList
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.TotalCount != 1 || listing.Limit != 1 || len(listing.Evidence) != 1 || listing.Evidence[0].ID != fixture.evidenceID {
		t.Fatalf("unexpected paged result: %#v", listing)
	}
	response = request(handler, http.MethodGet, "/api/v1/verifications?limit=1&status=VERIFIED", "", nil)
	var verifications VerificationList
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &verifications) != nil || verifications.TotalCount != 0 {
		t.Fatalf("status filter mismatch: %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/v1/evidence?limit=0", "/api/v1/evidence?limit=201", "/api/v1/evidence?offset=-1", "/api/v1/evidence?file=/etc/passwd", "/api/v1/evidence?limit=1&limit=2"} {
		if got := request(handler, http.MethodGet, path, "", nil); got.Code != http.StatusBadRequest {
			t.Fatalf("invalid list query %q returned %d: %s", path, got.Code, got.Body.String())
		}
	}
}

func TestF15OversizedMutationFailsClosed(t *testing.T) {
	service, _ := seededService(t)
	handler, err := NewHandler(service)
	if err != nil {
		t.Fatal(err)
	}
	sessionResponse := request(handler, http.MethodGet, "/api/v1/session", "", nil)
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	body := `{"policy":"manual","padding":"` + strings.Repeat("x", maxMutationBody) + `"}`
	response := request(handler, http.MethodPost, "/api/v1/governance/update-policy", body, map[string]string{"X-SwipeNode-CSRF": session.CSRFToken, "Origin": "http://127.0.0.1"})
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized mutation returned %d: %s", response.Code, response.Body.String())
	}
}

func TestOpenAPIDocumentsEveryControlPlaneOperation(t *testing.T) {
	expected := []string{
		"GET /api/v1/audit", "GET /api/v1/audit/{id}", "GET /api/v1/evidence", "GET /api/v1/evidence/{id}",
		"GET /api/v1/knowledge/packs", "GET /api/v1/knowledge/packs/{id}", "GET /api/v1/knowledge/validate",
		"GET /api/v1/overview", "GET /api/v1/provenance", "GET /api/v1/provenance/{id}", "GET /api/v1/session",
		"GET /api/v1/verifications", "GET /api/v1/verifications/{id}",
		"POST /api/v1/governance/update-policy", "POST /api/v1/knowledge/packs/{id}/activate",
		"POST /api/v1/knowledge/packs/{id}/pull", "POST /api/v1/knowledge/packs/{id}/refresh",
		"POST /api/v1/knowledge/packs/{id}/rollback", "POST /api/v1/knowledge/packs/{id}/update-check",
	}
	if operations := openAPIOperations(t, OpenAPIDocument()); !reflect.DeepEqual(operations, expected) {
		t.Fatalf("documented operations do not match the registered Control Plane surface\n got: %#v\nwant: %#v", operations, expected)
	}
}

func openAPIOperations(t *testing.T, document []byte) []string {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(document, &spec); err != nil {
		t.Fatalf("parse embedded OpenAPI: %v", err)
	}
	operations := []string{}
	for path, definition := range spec.Paths {
		for method := range definition {
			if method == "get" || method == "post" || method == "put" || method == "patch" || method == "delete" {
				operations = append(operations, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(operations)
	return operations
}

func TestLoopbackHostBoundary(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8082", "[::1]:8082", "localhost:8082"} {
		if !safeLocalHost(host) {
			t.Fatalf("local Host %q rejected", host)
		}
	}
	for _, host := range []string{"localhost.evil.example:8082", "127.0.0.1.evil:8082", "0.0.0.0:8082", "192.0.2.2:8082"} {
		if safeLocalHost(host) {
			t.Fatalf("deceptive/non-local Host %q accepted", host)
		}
	}
}

func request(handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
