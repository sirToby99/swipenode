package controlplane

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sirToby99/swipenode/internal/distribution"
)

const maxMutationBody = 16 << 10

//go:embed assets/* openapi.yaml
var embeddedFiles embed.FS

type API struct {
	Service   *Service
	CSRFToken string
}

func NewHandler(service *Service) (http.Handler, error) {
	if service == nil {
		return nil, fmt.Errorf("control-plane service is required")
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("create control-plane CSRF token: %w", err)
	}
	return &API{Service: service, CSRFToken: hex.EncodeToString(tokenBytes)}, nil
}

func (api *API) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setHeaders(response)
	if !safeLocalHost(request.Host) {
		writeAPIError(response, http.StatusForbidden, "host_not_allowed", "Control Plane requests require a loopback Host header.")
		return
	}
	if request.URL.RawQuery != "" && (request.Method != http.MethodGet || !paginatedListRoute(request.URL.Path)) {
		writeAPIError(response, http.StatusBadRequest, "query_not_allowed", "Control Plane routes do not accept URL query parameters.")
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		response.Header().Set("Allow", "GET, POST")
		writeAPIError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only documented GET and POST operations are supported.")
		return
	}
	if request.Method == http.MethodPost {
		if !api.validMutationRequest(request) {
			writeAPIError(response, http.StatusForbidden, "csrf_rejected", "Mutation requires a same-origin SwipeNode CSRF token.")
			return
		}
		if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
			writeAPIError(response, http.StatusUnsupportedMediaType, "content_type_required", "Mutation Content-Type must be application/json.")
			return
		}
	}
	if strings.HasPrefix(request.URL.Path, "/api/v1/") {
		api.serveAPI(response, request)
		return
	}
	if request.Method != http.MethodGet {
		writeAPIError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Static Control Plane assets are read-only.")
		return
	}
	switch request.URL.Path {
	case "/":
		api.serveAsset(response, request, "index.html")
	case "/assets/app.css":
		api.serveAsset(response, request, "app.css")
	case "/assets/app.js":
		api.serveAsset(response, request, "app.js")
	case "/openapi.yaml":
		data, _ := embeddedFiles.ReadFile("openapi.yaml")
		response.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(data)
	default:
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane route not found.")
	}
}

func (api *API) serveAsset(response http.ResponseWriter, request *http.Request, name string) {
	data, err := embeddedFiles.ReadFile("assets/" + name)
	if err != nil {
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane asset not found.")
		return
	}
	contentType := "application/octet-stream"
	switch {
	case strings.HasSuffix(name, ".html"):
		contentType = "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		contentType = "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		contentType = "text/javascript; charset=utf-8"
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(data)
}

func (api *API) serveAPI(response http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	if request.Method == http.MethodGet {
		switch path {
		case "/api/v1/session":
			writeAPIJSON(response, http.StatusOK, struct {
				SchemaVersion  string `json:"schema_version"`
				CSRFToken      string `json:"csrf_token"`
				EntireOptional bool   `json:"entire_optional"`
			}{"swipenode.control-plane-session.v1", api.CSRFToken, true})
			return
		case "/api/v1/overview":
			value, err := api.Service.Overview(request.Context())
			api.writeResult(response, value, err)
			return
		case "/api/v1/knowledge/packs":
			value, err := api.Service.Packs(request.Context())
			api.writeResult(response, value, err)
			return
		case "/api/v1/knowledge/validate":
			value, err := api.Service.Validate(request.Context())
			api.writeResult(response, value, err)
			return
		case "/api/v1/evidence":
			options, ok := parseListOptions(response, request)
			if !ok {
				return
			}
			value, err := api.Service.EvidencePage(options)
			api.writeResult(response, value, err)
			return
		case "/api/v1/verifications":
			options, ok := parseListOptions(response, request)
			if !ok {
				return
			}
			value, err := api.Service.VerificationsPage(options)
			api.writeResult(response, value, err)
			return
		case "/api/v1/provenance":
			options, ok := parseListOptions(response, request)
			if !ok {
				return
			}
			value, err := api.Service.ProvenancePage(options)
			api.writeResult(response, value, err)
			return
		case "/api/v1/audit":
			options, ok := parseListOptions(response, request)
			if !ok {
				return
			}
			value, err := api.Service.AuditPage(options)
			api.writeResult(response, value, err)
			return
		}
		if id, ok := exactIDPath(path, "/api/v1/knowledge/packs/"); ok {
			value, found, err := api.Service.Pack(request.Context(), id)
			api.writeFound(response, value, found, err)
			return
		}
		if id, ok := exactIDPath(path, "/api/v1/evidence/"); ok {
			value, found, err := api.Service.FindEvidence(id)
			api.writeFound(response, value, found, err)
			return
		}
		if id, ok := exactIDPath(path, "/api/v1/verifications/"); ok {
			value, found, err := api.Service.FindVerification(id)
			api.writeFound(response, value, found, err)
			return
		}
		if id, ok := exactIDPath(path, "/api/v1/provenance/"); ok {
			value, found, err := api.Service.FindProvenance(id)
			api.writeFound(response, value, found, err)
			return
		}
		if id, ok := exactIDPath(path, "/api/v1/audit/"); ok {
			value, found, err := api.Service.FindAudit(id)
			api.writeFound(response, value, found, err)
			return
		}
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane API route not found.")
		return
	}
	api.serveMutation(response, request)
}

func paginatedListRoute(path string) bool {
	switch path {
	case "/api/v1/evidence", "/api/v1/verifications", "/api/v1/provenance", "/api/v1/audit":
		return true
	default:
		return false
	}
}

func parseListOptions(response http.ResponseWriter, request *http.Request) (ListOptions, bool) {
	if request.URL.RawQuery == "" {
		return ListOptions{}, true
	}
	values := request.URL.Query()
	for key, entries := range values {
		if key != "limit" && key != "offset" && key != "q" && key != "status" {
			writeAPIError(response, http.StatusBadRequest, "query_not_allowed", "Only list pagination and local filtering parameters are supported.")
			return ListOptions{}, false
		}
		if len(entries) != 1 {
			writeAPIError(response, http.StatusBadRequest, "invalid_query", "List parameters must appear exactly once.")
			return ListOptions{}, false
		}
	}
	options := ListOptions{Limit: 50, Query: strings.TrimSpace(values.Get("q")), Status: strings.TrimSpace(values.Get("status"))}
	if len(options.Query) > 256 || len(options.Status) > 64 {
		writeAPIError(response, http.StatusBadRequest, "invalid_query", "List filter is too long.")
		return ListOptions{}, false
	}
	if raw := values.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeAPIError(response, http.StatusBadRequest, "invalid_query", "List limit must be between 1 and 200.")
			return ListOptions{}, false
		}
		options.Limit = value
	}
	if raw := values.Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1_000_000 {
			writeAPIError(response, http.StatusBadRequest, "invalid_query", "List offset must be between 0 and 1000000.")
			return ListOptions{}, false
		}
		options.Offset = value
	}
	return options, true
}

func (api *API) serveMutation(response http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	if path == "/api/v1/governance/update-policy" {
		var body struct {
			Policy distribution.UpdatePolicy `json:"policy"`
		}
		if !decodeMutation(response, request, &body) {
			return
		}
		if err := api.Service.SetPolicy(body.Policy); err != nil {
			writeAPIError(response, http.StatusBadRequest, "policy_rejected", err.Error())
			return
		}
		writeAPIJSON(response, http.StatusOK, struct {
			SchemaVersion string                    `json:"schema_version"`
			Policy        distribution.UpdatePolicy `json:"policy"`
		}{"swipenode.control-plane-policy.v1", body.Policy})
		return
	}
	prefix := "/api/v1/knowledge/packs/"
	if !strings.HasPrefix(path, prefix) {
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane mutation route not found.")
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 || !safeRouteID(parts[0]) {
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane mutation route not found.")
		return
	}
	packID, action := parts[0], parts[1]
	switch action {
	case "update-check":
		var body struct {
			RemoteID string `json:"remote_id,omitempty"`
		}
		if !decodeMutation(response, request, &body) {
			return
		}
		value, err := api.Service.UpdateCheck(request.Context(), packID, body.RemoteID)
		api.writeAction(response, value, err)
	case "pull":
		var body struct {
			RemoteID string `json:"remote_id,omitempty"`
		}
		if !decodeMutation(response, request, &body) {
			return
		}
		release, installed, err := api.Service.Pull(request.Context(), packID, body.RemoteID)
		api.writeAction(response, struct {
			Release   distribution.InstalledRelease `json:"release"`
			Installed bool                          `json:"installed"`
		}{release, installed}, err)
	case "activate":
		var body struct {
			Version string `json:"version"`
		}
		if !decodeMutation(response, request, &body) {
			return
		}
		value, err := api.Service.Activate(packID, body.Version)
		api.writeAction(response, value, err)
	case "rollback":
		var body struct{}
		if !decodeMutation(response, request, &body) {
			return
		}
		value, err := api.Service.Rollback(packID)
		api.writeAction(response, value, err)
	case "refresh":
		var body RefreshRequest
		if !decodeMutation(response, request, &body) {
			return
		}
		value, err := api.Service.Refresh(request.Context(), packID, body)
		api.writeAction(response, value, err)
	default:
		writeAPIError(response, http.StatusNotFound, "not_found", "Control Plane mutation route not found.")
	}
}

func (api *API) validMutationRequest(request *http.Request) bool {
	if request.Header.Get("X-SwipeNode-CSRF") != api.CSRFToken || request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.ParseRequestURI(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.User == nil && parsed.Host == request.Host && parsed.RawQuery == "" && parsed.Fragment == ""
}

func safeLocalHost(value string) bool {
	host := value
	if parsedHost, _, err := net.SplitHostPort(value); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func safeRouteID(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\") {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func exactIDPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(path, prefix)
	return id, safeRouteID(id)
}

func decodeMutation(response http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxMutationBody)
	defer request.Body.Close()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maximum *http.MaxBytesError
		if errors.As(err, &maximum) {
			writeAPIError(response, http.StatusRequestEntityTooLarge, "payload_too_large", "Mutation body exceeds 16 KiB.")
		} else {
			writeAPIError(response, http.StatusBadRequest, "invalid_json", "Mutation body must be one documented JSON object.")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(response, http.StatusBadRequest, "invalid_json", "Mutation body must contain exactly one JSON value.")
		return false
	}
	return true
}

func setHeaders(response http.ResponseWriter) {
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Frame-Options", "DENY")
	response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("Cache-Control", "no-store")
}

func writeAPIJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeAPIError(response http.ResponseWriter, status int, code, message string) {
	writeAPIJSON(response, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message}})
}

func (api *API) writeResult(response http.ResponseWriter, value any, err error) {
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "local_state_error", "Local SwipeNode state failed validation.")
		return
	}
	writeAPIJSON(response, http.StatusOK, value)
}

func (api *API) writeFound(response http.ResponseWriter, value any, found bool, err error) {
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "local_state_error", "Local SwipeNode state failed validation.")
	} else if !found {
		writeAPIError(response, http.StatusNotFound, "not_found", "Local SwipeNode record not found.")
	} else {
		writeAPIJSON(response, http.StatusOK, value)
	}
}

func (api *API) writeAction(response http.ResponseWriter, value any, err error) {
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "action_rejected", err.Error())
		return
	}
	writeAPIJSON(response, http.StatusOK, value)
}

func OpenAPIDocument() []byte {
	data, _ := embeddedFiles.ReadFile("openapi.yaml")
	return append([]byte(nil), data...)
}
