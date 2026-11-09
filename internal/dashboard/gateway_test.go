package dashboard

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerGatewayAllowsOnlyListedContainersAndApprovedActions(t *testing.T) {
	definitions := []ServiceDefinition{
		{ID: "tool", Name: "Tool", Project: "tool", Category: CategoryTools, Lifecycle: LifecycleOnDemand, Control: ControlPower},
		{ID: "core", Name: "Core", Project: "core", Category: CategoryInfrastructure, Lifecycle: LifecycleAlwaysOn},
	}
	operator := &recordingOperator{containers: []DockerContainer{
		{ID: "allowed123", Names: []string{"/tool"}, State: "exited", Labels: map[string]string{composeProjectLabel: "tool"}},
		{ID: "readonly123", Names: []string{"/core"}, State: "running", Labels: map[string]string{composeProjectLabel: "core"}},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewDockerGateway(definitions, operator, logger).Router()

	assertGatewayStatus(t, router, http.MethodGet, "/healthz", "", http.StatusOK)
	assertGatewayStatus(t, router, http.MethodGet, "/containers/json?all=1", "", http.StatusOK)
	assertGatewayStatus(t, router, http.MethodGet, "/containers/json", "", http.StatusBadRequest)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/allowed123/start", "", http.StatusNoContent)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/readonly123/stop", "", http.StatusForbidden)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/missing/start", "", http.StatusNotFound)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/allowed123/remove", "", http.StatusForbidden)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/create", "", http.StatusNotFound)
	assertGatewayStatus(t, router, http.MethodPost, "/containers/allowed123/start", "payload", http.StatusBadRequest)
	assertGatewayStatus(t, router, http.MethodGet, "/containers/allowed123/start", "", http.StatusMethodNotAllowed)

	if len(operator.actions) != 1 || operator.actions[0] != "start:allowed123" {
		t.Fatalf("unexpected Docker operations: %#v", operator.actions)
	}
}

func TestDockerGatewayForwardsOnlyTheComposeProjectLabel(t *testing.T) {
	secretLabel := "traefik.http.middlewares.janitor-auth.basicauth.users"
	operator := &recordingOperator{containers: []DockerContainer{{
		ID:     "core123",
		Names:  []string{"/core"},
		Image:  "core:1",
		State:  "running",
		Status: "Up 2 days (healthy)",
		Labels: map[string]string{
			composeProjectLabel: "core",
			secretLabel:         "admin:$apr1$hash",
			"com.example.token": "value",
		},
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewDockerGateway(nil, operator, logger).Router()

	request := httptest.NewRequest(http.MethodGet, "/containers/json?all=1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list containers: expected 200, got %d", response.Code)
	}

	body := response.Body.String()
	if strings.Contains(body, secretLabel) || strings.Contains(body, "com.example.token") {
		t.Fatalf("gateway leaked non-project labels: %s", body)
	}

	var decoded []DockerContainer
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode gateway response: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected one container, got %d", len(decoded))
	}
	container := decoded[0]
	if container.ID != "core123" || container.Image != "core:1" || container.Status != "Up 2 days (healthy)" {
		t.Fatalf("gateway dropped fields the dashboard needs: %#v", container)
	}
	if len(container.Labels) != 1 || container.Labels[composeProjectLabel] != "core" {
		t.Fatalf("expected only the Compose project label, got %#v", container.Labels)
	}
}

func assertGatewayStatus(
	t *testing.T,
	handler http.Handler,
	method string,
	target string,
	body string,
	expected int,
) {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != expected {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, target, expected, response.Code, response.Body.String())
	}
}
