// Package fakerso is an in-memory stand-in for the RSO cloud API, for tests:
// the identity provider's token and device endpoints, the GraphQL endpoint
// and the REST data routes. It keeps real state for queues, topics,
// subscriptions and scheduled jobs, and answers the rest from fixtures a
// test can replace.
package fakerso

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Names used by tests. They are placeholders, not real accounts or tenants.
const (
	AccessToken    = "test-access-token"
	RefreshedToken = "test-refreshed-token"
	RefreshToken   = "test-refresh-token"
	ServiceAccount = "ci-deployer"
	ServiceSecret  = "test-service-secret"
	TenantA        = "11111111-1111-4111-8111-111111111111"
	TenantB        = "22222222-2222-4222-8222-222222222222"
)

// Call is one GraphQL operation the server received.
type Call struct {
	Field     string
	Variables map[string]any
	Tenant    string
	Region    string
}

// Handler answers one GraphQL root field: the value for `data.<field>`, or
// an error.
type Handler func(variables map[string]any) (any, *Error)

// Error is a GraphQL error with the platform's code.
type Error struct {
	Message string
	Code    string
}

// Server is the fake API.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	calls    []Call
	handlers map[string]Handler
	// Tokens the API accepts as a Bearer token.
	valid map[string]bool
	// DevicePending is how many polls answer authorization_pending before
	// the device sign-in succeeds.
	DevicePending int
	devicePolls   int
	// Configuration, when set, is served as the configuration document.
	Configuration map[string]string
	// ConfidentialClient is a client id the device sign-in is refused for.
	ConfidentialClient string
	clientIDs          []string

	queues   map[string]map[string]any
	messages map[string][]map[string]any
	topics   map[string]map[string]any
	subs     []map[string]any
	jobs     map[string]map[string]any
	apps     map[string]map[string]any
}

// New starts a fake API. Close it when the test is done.
func New() *Server {
	s := &Server{
		handlers: map[string]Handler{},
		valid:    map[string]bool{AccessToken: true, RefreshedToken: true},
		queues:   map[string]map[string]any{},
		messages: map[string][]map[string]any{},
		topics:   map[string]map[string]any{},
		jobs:     map[string]map[string]any{},
		apps:     map[string]map[string]any{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token/", s.token)
	mux.HandleFunc("/device/", s.device)
	mux.HandleFunc("/.well-known/rso-configuration", s.configuration)
	mux.HandleFunc("/graphql", s.graphql)
	mux.HandleFunc("/queues/v1/", s.data)
	mux.HandleFunc("/ai/v1/chat/completions", s.chat)
	s.Server = httptest.NewServer(mux)
	s.defaults()
	return s
}

// APIURL is the GraphQL endpoint.
func (s *Server) APIURL() string { return s.URL + "/graphql" }

// TokenURL is the identity provider's token endpoint.
func (s *Server) TokenURL() string { return s.URL + "/token/" }

// Handle replaces the answer for one GraphQL root field.
func (s *Server) Handle(field string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[field] = handler
}

// Fixed answers a root field with the same value every time.
func (s *Server) Fixed(field string, value any) {
	s.Handle(field, func(map[string]any) (any, *Error) { return value, nil })
}

// Fail makes a root field answer with an error.
func (s *Server) Fail(field, code, message string) {
	s.Handle(field, func(map[string]any) (any, *Error) { return nil, &Error{Message: message, Code: code} })
}

// Calls returns the GraphQL operations received so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// LastCall returns the most recent call of a root field.
func (s *Server) LastCall(field string) (Call, bool) {
	calls := s.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Field == field {
			return calls[i], true
		}
	}
	return Call{}, false
}

// Revoke makes the API refuse a token from now on.
func (s *Server) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.valid, token)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// configuration serves the platform's configuration document once
// Configuration is set; until then there is none, as on an older gateway.
func (s *Server) configuration(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	doc := s.Configuration
	s.mu.Unlock()
	if doc == nil {
		http.NotFound(w, nil)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// ClientIDs returns the client ids the token endpoint was called with.
func (s *Server) ClientIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.clientIDs...)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	s.clientIDs = append(s.clientIDs, r.Form.Get("client_id"))
	confidential := s.ConfidentialClient != "" && r.Form.Get("client_id") == s.ConfidentialClient
	s.mu.Unlock()
	refuse := func(code, description string) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": description})
	}
	switch r.Form.Get("grant_type") {
	case "client_credentials":
		if r.Form.Get("username") != ServiceAccount || r.Form.Get("password") != ServiceSecret {
			refuse("invalid_grant", "unknown service account or wrong secret")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": AccessToken, "expires_in": 600})
	case "refresh_token":
		if r.Form.Get("refresh_token") != RefreshToken {
			refuse("invalid_grant", "the refresh token is no longer valid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": RefreshedToken, "refresh_token": RefreshToken, "expires_in": 600})
	case "urn:ietf:params:oauth:grant-type:device_code":
		if confidential {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "Client authentication failed"})
			return
		}
		s.mu.Lock()
		s.devicePolls++
		pending := s.devicePolls <= s.DevicePending
		s.mu.Unlock()
		if r.Form.Get("device_code") != "test-device-code" {
			refuse("invalid_grant", "unknown device code")
			return
		}
		if pending {
			refuse("authorization_pending", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": AccessToken, "refresh_token": RefreshToken, "expires_in": 600})
	default:
		refuse("unsupported_grant_type", "")
	}
}

func (s *Server) device(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": "test-device-code", "user_code": "ABCD-1234",
		"verification_uri":          s.URL + "/device",
		"verification_uri_complete": s.URL + "/device?code=ABCD-1234",
		"expires_in":                60, "interval": 1,
	})
}

func (s *Server) authorized(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.valid[token]
}

var rootField = regexp.MustCompile(`^\s*(?:query|mutation)?[^{]*\{\s*(\w+)`)

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, code, message string) {
		writeJSON(w, status, map[string]any{"errors": []map[string]any{{"message": message, "extensions": map[string]any{"code": code}}}})
	}
	if !s.authorized(r) {
		fail(http.StatusOK, "AUTHENTICATION_ERROR", "You must be signed in")
		return
	}
	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	raw, _ := io.ReadAll(r.Body)
	if json.Unmarshal(raw, &request) != nil {
		fail(http.StatusBadRequest, "BAD_REQUEST", "the body is not JSON")
		return
	}
	match := rootField.FindStringSubmatch(request.Query)
	if match == nil {
		fail(http.StatusBadRequest, "GRAPHQL_VALIDATION_FAILED", "no root field")
		return
	}
	field := match[1]
	s.mu.Lock()
	s.calls = append(s.calls, Call{Field: field, Variables: request.Variables, Tenant: r.Header.Get("X-Tenant"), Region: r.Header.Get("X-Region")})
	handler := s.handlers[field]
	s.mu.Unlock()
	if handler == nil {
		fail(http.StatusOK, "GRAPHQL_VALIDATION_FAILED", fmt.Sprintf("Cannot query field %q", field))
		return
	}
	value, failure := handler(request.Variables)
	if failure != nil {
		fail(http.StatusOK, failure.Code, failure.Message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{field: value}})
}

// data serves the queue data routes: publish to a queue or topic, receive.
func (s *Server) data(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/queues/v1/"), "/")
	if len(parts) != 3 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var body map[string]any
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	kind, name, action := parts[0], parts[1], parts[2]

	s.mu.Lock()
	defer s.mu.Unlock()
	message := func(topic, key string) map[string]any {
		text, _ := body["body"].(string)
		if document, ok := body["json"]; ok {
			encoded, _ := json.Marshal(document)
			text = string(encoded)
		}
		return map[string]any{"body": text, "encoding": "utf8", "contentType": body["contentType"], "attributes": body["attributes"],
			"messageId": body["messageId"], "publishedAt": "2026-01-02T03:04:05Z", "topic": topic, "routingKey": key, "redelivered": false}
	}
	switch {
	case kind == "queues" && action == "messages":
		if _, ok := s.queues[name]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Queue " + name + " not found", "code": "NOT_FOUND"})
			return
		}
		s.messages[name] = append(s.messages[name], message("", name))
		writeJSON(w, http.StatusOK, map[string]bool{"routed": true})
	case kind == "topics" && action == "messages":
		if _, ok := s.topics[name]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Topic " + name + " not found", "code": "NOT_FOUND"})
			return
		}
		key, _ := body["routingKey"].(string)
		routed := false
		for _, sub := range s.subs {
			if sub["topic"] == name && matches(sub["filter"].(string), key) {
				queue := sub["target"].(map[string]any)["queue"].(string)
				s.messages[queue] = append(s.messages[queue], message(name, key))
				routed = true
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"routed": routed})
	case kind == "queues" && action == "receive":
		if _, ok := s.queues[name]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Queue " + name + " not found", "code": "NOT_FOUND"})
			return
		}
		limit := 1
		if n, ok := body["max"].(float64); ok {
			limit = int(n)
		}
		waiting := s.messages[name]
		limit = min(limit, len(waiting))
		s.messages[name] = waiting[limit:]
		writeJSON(w, http.StatusOK, map[string]any{"messages": waiting[:limit]})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// matches implements the routing key patterns: * is one word, # any number.
func matches(filter, key string) bool {
	if filter == "#" || filter == "" {
		return true
	}
	pattern := regexp.QuoteMeta(filter)
	pattern = strings.ReplaceAll(pattern, `\*`, `[^.]+`)
	pattern = strings.ReplaceAll(pattern, `#`, `.*`)
	ok, _ := regexp.MatchString("^"+pattern+"$", key)
	return ok
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	if body.Model == "over-quota" {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]string{"message": "the monthly inference token quota of this tenant is used up", "type": "insufficient_quota", "code": "insufficient_quota"}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, ": queued position=1\n\n")
	for _, piece := range []string{"Hello", ", ", "world"} {
		chunk, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": piece}}}})
		_, _ = io.WriteString(w, "data: "+string(chunk)+"\n\n")
	}
	_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3}}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func sorted(items map[string]map[string]any) []map[string]any {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, items[name])
	}
	return out
}
