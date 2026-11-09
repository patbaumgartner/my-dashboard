package dashboard

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRouterServesAPIAndFrontend(t *testing.T) {
	definitions := []ServiceDefinition{{ID: "tool", Name: "Tool", Project: "tool", Lifecycle: LifecycleOnDemand}}
	operator := &recordingOperator{}
	reader := newServiceReader(definitions, operator)
	staticFiles := fstest.MapFS{
		"index.html":     {Data: []byte("dashboard")},
		"assets/app.js":  {Data: []byte("app")},
		"assets/app.css": {Data: []byte("style")},
	}
	router := NewRouter(reader, NewServiceController(definitions, operator), staticFiles, slog.Default())

	servicesRequest := httptest.NewRequest(http.MethodGet, "/mydashboard/api/services", nil)
	servicesResponse := httptest.NewRecorder()
	router.ServeHTTP(servicesResponse, servicesRequest)
	if servicesResponse.Code != http.StatusOK {
		t.Fatalf("expected services 200, got %d", servicesResponse.Code)
	}
	var payload ServicesResponse
	if err := json.Unmarshal(servicesResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Services) != 1 || payload.Services[0].ID != "tool" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if servicesResponse.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected security headers")
	}
	frontendRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	frontendResponse := httptest.NewRecorder()
	router.ServeHTTP(frontendResponse, frontendRequest)
	if frontendResponse.Code != http.StatusOK || frontendResponse.Body.String() != "dashboard" {
		t.Fatalf("unexpected frontend response: %d %q", frontendResponse.Code, frontendResponse.Body.String())
	}
}

func TestServiceResponseIncludesPresentationConfiguration(t *testing.T) {
	manifest := Manifest{
		LifecycleFilters: []LifecycleFilterDefinition{{ID: "all", Label: "All services", ShortLabel: "All"}},
		Categories:       []CategoryDefinition{{ID: CategoryTools, Title: "Tools", Description: "Utilities"}},
		QuickLinks:       []QuickLinkDefinition{{ID: "router", Name: "Router", URL: "https://router.example", Icon: "router"}},
		Footer:           FooterDefinition{Year: 2026, Name: "Patrick", URL: "https://example.com", Message: "Crafted with heart"},
		Services:         []ServiceDefinition{{ID: "tool", Name: "Tool", Project: "tool", Category: CategoryTools, Lifecycle: LifecycleAlwaysOn}},
	}
	operator := &recordingOperator{}
	reader := NewServiceReader(manifest, operator, nil)
	response := reader.Read(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	if len(response.LifecycleFilters) != 1 || len(response.Categories) != 1 || len(response.QuickLinks) != 1 || response.Footer.Name != "Patrick" {
		t.Fatalf("unexpected presentation response: %#v", response)
	}
}

func TestRouterRejectsStateChangingRequests(t *testing.T) {
	operator := &recordingOperator{}
	reader := newServiceReader(nil, operator)
	router := NewRouter(reader, NewServiceController(nil, operator), fstest.MapFS{"index.html": {Data: []byte("dashboard")}}, slog.Default())

	for _, endpoint := range []string{"/api/services", "/mydashboard/api/services", "/healthz", "/mydashboard/healthz", "/mydashboard/", "/"} {
		request := httptest.NewRequest(http.MethodPost, endpoint, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected POST %s to return 405, got %d", endpoint, response.Code)
		}
	}
}

func TestRouterRequiresSameOriginConfirmationForActions(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "tool", Name: "Tool", Project: "tool", Lifecycle: LifecycleOnDemand, Control: ControlPower,
	}}
	operator := &recordingOperator{containers: []DockerContainer{{
		ID: "tool-id", Names: []string{"/tool"}, State: "exited", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	reader := newServiceReader(definitions, operator)
	router := NewRouter(reader, NewServiceController(definitions, operator), fstest.MapFS{"index.html": {Data: []byte("dashboard")}}, slog.Default())

	for _, test := range []struct {
		name   string
		origin string
		header string
		status int
	}{
		{name: "missing confirmation", origin: "http://example.com", status: http.StatusForbidden},
		{name: "cross origin", origin: "https://attacker.example", header: "confirm", status: http.StatusForbidden},
		{name: "same origin", origin: "http://example.com", header: "confirm", status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://example.com/mydashboard/api/services/tool/actions/start", nil)
			request.Header.Set("Origin", test.origin)
			if test.header != "" {
				request.Header.Set(actionConfirmationHeader, test.header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("expected %d, got %d: %s", test.status, response.Code, response.Body.String())
			}
		})
	}

	if len(operator.actions) != 1 || operator.actions[0] != "start:tool-id" {
		t.Fatalf("unexpected Docker operations: %#v", operator.actions)
	}
}

func TestRouterServesSingleServiceAvailability(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example",
		Lifecycle: LifecycleOnDemand, StartupTimeoutSeconds: 42,
	}}
	operator := &recordingOperator{containers: []DockerContainer{{
		ID: "tool", State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	checker := &recordingAvailabilityChecker{available: true}
	reader := newServiceReaderWithAvailability(definitions, operator, checker)
	router := NewRouter(reader, NewServiceController(definitions, operator), fstest.MapFS{"index.html": {Data: []byte("dashboard")}}, slog.Default())

	request := httptest.NewRequest(http.MethodGet, "/mydashboard/api/services/tool/availability", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected availability 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload AvailabilityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Available || payload.State != "running" || payload.ServiceID != "tool" || payload.URL != "https://tool.example" || payload.StartupTimeoutSeconds != 42 {
		t.Fatalf("unexpected availability payload: %#v", payload)
	}
	if checker.calls.Load() != 1 {
		t.Fatalf("expected one availability probe, got %d", checker.calls.Load())
	}

	missing := httptest.NewRequest(http.MethodGet, "/api/services/unknown/availability", nil)
	missingResponse := httptest.NewRecorder()
	router.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("expected unknown service 404, got %d", missingResponse.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/services/tool/availability", nil)
	postResponse := httptest.NewRecorder()
	router.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST availability 405, got %d", postResponse.Code)
	}
}

func TestSPAHandlerFallsBackToIndex(t *testing.T) {
	files := fstest.MapFS{"index.html": {Data: []byte("dashboard")}}
	request := httptest.NewRequest(http.MethodGet, "/not-found", nil)
	response := httptest.NewRecorder()
	spaHandler(fs.FS(files)).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "dashboard" {
		t.Fatalf("unexpected fallback response: %d %q", response.Code, response.Body.String())
	}
}

func TestSPAHandlerDoesNotListDirectories(t *testing.T) {
	files := fstest.MapFS{
		"index.html":       {Data: []byte("dashboard")},
		"assets/app.js":    {Data: []byte("app")},
		"assets/secret.js": {Data: []byte("secret")},
	}
	for _, target := range []string{"/assets", "/assets/"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		spaHandler(fs.FS(files)).ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != http.StatusOK || body != "dashboard" {
			t.Fatalf("%s: expected the SPA entry point, got %d %q", target, response.Code, body)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	response := httptest.NewRecorder()
	spaHandler(fs.FS(files)).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "app" {
		t.Fatalf("real assets must still be served, got %d %q", response.Code, response.Body.String())
	}
}

func TestStaticAssetsDoNotFallBackToIndex(t *testing.T) {
	files := fstest.MapFS{
		"index.html":      {Data: []byte("dashboard")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
	}
	router := NewRouter(newServiceReader(nil, &stubContainerSource{}), NewServiceController(nil, &recordingOperator{}), files, slog.Default())

	for _, test := range []struct {
		path, cache string
		code        int
	}{
		{"/assets/app-1.js", "public, max-age=31536000, immutable", http.StatusOK},
		{"/assets/app-old.js", "", http.StatusNotFound},
		{"/", "no-cache", http.StatusOK},
		{"/starting", "no-cache", http.StatusOK},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.code || response.Header().Get("Cache-Control") != test.cache {
			t.Fatalf("%s: code=%d cache=%q", test.path, response.Code, response.Header().Get("Cache-Control"))
		}
	}
}

func TestActionErrorsDoNotLeakInternals(t *testing.T) {
	message := actionErrorMessage(errors.New("Docker start: status 500: http://docker-gateway:2375 secret"))
	if strings.Contains(message, "docker-gateway") || message == "" {
		t.Fatalf("unexpected client message %q", message)
	}
}
