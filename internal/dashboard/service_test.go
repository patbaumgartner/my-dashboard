package dashboard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type stubContainerSource struct {
	containers []DockerContainer
	err        error
	calls      int
}

func (source *stubContainerSource) ListContainers(context.Context) ([]DockerContainer, error) {
	source.calls++
	return source.containers, source.err
}

type recordingAvailabilityChecker struct {
	available bool
	calls     atomic.Int32
}

func (checker *recordingAvailabilityChecker) Check(context.Context, string) (bool, string) {
	checker.calls.Add(1)
	return checker.available, "checked"
}

func newServiceReader(definitions []ServiceDefinition, source ContainerSource) *ServiceReader {
	return newServiceReaderWithAvailability(definitions, source, nil)
}

func newServiceReaderWithAvailability(
	definitions []ServiceDefinition,
	source ContainerSource,
	availability AvailabilityChecker,
) *ServiceReader {
	return NewServiceReader(Manifest{Services: definitions}, source, availability)
}

func TestServiceReaderGroupsComposeProjects(t *testing.T) {
	definitions := []ServiceDefinition{
		{ID: "media", Name: "Media", Project: "media", Category: CategoryMedia, Lifecycle: LifecycleAlwaysOn},
		{ID: "tool", Name: "Tool", Project: "tool", Category: CategoryTools, Lifecycle: LifecycleOnDemand},
		{ID: "old", Name: "Old", Project: "old", Category: CategoryInfrastructure, Lifecycle: LifecycleAlwaysOn, Hidden: true},
	}
	source := &stubContainerSource{containers: []DockerContainer{
		{Names: []string{"/media-app"}, Image: "media:1", State: "running", Status: "Up 2 hours (healthy)", Labels: map[string]string{composeProjectLabel: "media"}},
		{Names: []string{"/media-db"}, Image: "postgres:18", State: "running", Status: "Up 2 hours", Labels: map[string]string{composeProjectLabel: "media"}},
		{Names: []string{"/extra"}, Image: "extra:1", State: "exited", Status: "Exited (0)", Labels: map[string]string{composeProjectLabel: "new-tool"}},
	}}
	reader := newServiceReader(definitions, source)
	reader.now = func() time.Time { return time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC) }

	response := reader.Read(context.Background())
	if !response.DockerConnected || response.Error != "" {
		t.Fatalf("expected connected response, got %#v", response)
	}
	if len(response.Services) != 3 {
		t.Fatalf("expected two configured and one discovered service, got %d", len(response.Services))
	}

	media := response.Services[0]
	if media.Category != CategoryMedia || media.State != "healthy" || len(media.Containers) != 2 || media.ControlsLocked != true {
		t.Fatalf("unexpected media service: %#v", media)
	}
	tool := response.Services[1]
	if tool.State != "stopped" || len(tool.Containers) != 0 {
		t.Fatalf("unexpected on-demand service: %#v", tool)
	}
	discovered := response.Services[2]
	if discovered.ID != "discovered-new-tool" || discovered.Name != "New Tool" || discovered.Category != CategoryDiscovered || discovered.Lifecycle != LifecycleDiscovered || discovered.State != "stopped" {
		t.Fatalf("unexpected discovered service: %#v", discovered)
	}
}

func TestServiceReaderMarksPartialAndUnhealthyProjectsDegraded(t *testing.T) {
	definitions := []ServiceDefinition{
		{ID: "partial", Name: "Partial", Project: "partial", Lifecycle: LifecycleAlwaysOn},
		{ID: "unhealthy", Name: "Unhealthy", Project: "unhealthy", Lifecycle: LifecycleAlwaysOn},
	}
	source := &stubContainerSource{containers: []DockerContainer{
		{Names: []string{"/partial-app"}, State: "running", Status: "Up", Labels: map[string]string{composeProjectLabel: "partial"}},
		{Names: []string{"/partial-db"}, State: "exited", Status: "Exited", Labels: map[string]string{composeProjectLabel: "partial"}},
		{Names: []string{"/bad"}, State: "running", Status: "Up (unhealthy)", Labels: map[string]string{composeProjectLabel: "unhealthy"}},
	}}

	services := newServiceReader(definitions, source).Read(context.Background()).Services
	if services[0].State != "degraded" || services[0].Status != "1 of 2 running" {
		t.Fatalf("unexpected partial state: %#v", services[0])
	}
	if services[1].State != "degraded" || services[1].Status != "1 unhealthy" {
		t.Fatalf("unexpected unhealthy state: %#v", services[1])
	}
}

func TestServiceReaderKeepsConfiguredInventoryWhenDockerIsUnavailable(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "jellyfin", Name: "Jellyfin", Project: "jellyfin", Lifecycle: LifecycleAlwaysOn,
	}}
	reader := newServiceReader(definitions, &stubContainerSource{err: errors.New("offline")})

	response := reader.Read(context.Background())
	if response.DockerConnected || response.Error == "" {
		t.Fatalf("expected disconnected response, got %#v", response)
	}
	if len(response.Services) != 1 || response.Services[0].State != "unknown" {
		t.Fatalf("expected configured unknown service, got %#v", response.Services)
	}
}

func TestServiceReaderAdvertisesOnlyStateAppropriatePowerActions(t *testing.T) {
	definitions := []ServiceDefinition{
		{ID: "stopped", Name: "Stopped", Project: "stopped", Lifecycle: LifecycleOnDemand, Control: ControlPower},
		{ID: "running", Name: "Running", Project: "running", Lifecycle: LifecycleOnDemand, Control: ControlPower},
		{ID: "readonly", Name: "Readonly", Project: "readonly", Lifecycle: LifecycleAlwaysOn},
	}
	source := &stubContainerSource{containers: []DockerContainer{
		{ID: "stopped", State: "exited", Labels: map[string]string{composeProjectLabel: "stopped"}},
		{ID: "running", State: "running", Labels: map[string]string{composeProjectLabel: "running"}},
		{ID: "readonly", State: "running", Labels: map[string]string{composeProjectLabel: "readonly"}},
	}}

	services := newServiceReader(definitions, source).Read(context.Background()).Services
	if !reflect.DeepEqual(services[0].AvailableActions, []ServiceAction{ActionStart}) {
		t.Fatalf("unexpected stopped actions: %#v", services[0].AvailableActions)
	}
	if !reflect.DeepEqual(services[1].AvailableActions, []ServiceAction{ActionStop, ActionRestart}) {
		t.Fatalf("unexpected running actions: %#v", services[1].AvailableActions)
	}
	if !services[2].ControlsLocked || len(services[2].AvailableActions) != 0 {
		t.Fatalf("unexpected readonly controls: %#v", services[2])
	}
}

func TestServiceReaderRefreshesDockerAndAvailabilityEveryRead(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example", Lifecycle: LifecycleAlwaysOn,
	}}
	source := &stubContainerSource{containers: []DockerContainer{{
		ID: "tool", State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	checker := &recordingAvailabilityChecker{available: true}
	reader := newServiceReaderWithAvailability(definitions, source, checker)

	first := reader.Read(context.Background())
	second := reader.Read(context.Background())
	if source.calls != 2 || checker.calls.Load() != 2 {
		t.Fatalf("expected two fresh Docker and route checks, got Docker=%d route=%d", source.calls, checker.calls.Load())
	}
	if !first.Services[0].Available || !second.Services[0].Available {
		t.Fatalf("expected reachable service on both reads: %#v %#v", first.Services[0], second.Services[0])
	}
}

func TestServiceReaderDoesNotProbeStoppedService(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example", Lifecycle: LifecycleOnDemand,
	}}
	source := &stubContainerSource{containers: []DockerContainer{{
		ID: "tool", State: "exited", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	checker := &recordingAvailabilityChecker{available: true}
	service := newServiceReaderWithAvailability(definitions, source, checker).Read(context.Background()).Services[0]
	if checker.calls.Load() != 0 || service.Available || service.Availability != "Service not running" {
		t.Fatalf("unexpected stopped-service availability: calls=%d service=%#v", checker.calls.Load(), service)
	}
}

func TestServiceReaderMarksRunningUnavailableRouteDegraded(t *testing.T) {
	definitions := []ServiceDefinition{{
		ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example", CheckURL: "http://tool:8080/", Lifecycle: LifecycleAlwaysOn,
	}}
	source := &stubContainerSource{containers: []DockerContainer{{
		ID: "tool", State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	checker := &recordingAvailabilityChecker{available: false}
	service := newServiceReaderWithAvailability(definitions, source, checker).Read(context.Background()).Services[0]
	if service.Available || service.State != "degraded" || service.Status != "Containers running; Backend: checked" {
		t.Fatalf("unexpected unavailable-route state: %#v", service)
	}
}

func TestServiceReaderRequiresBackendAndBrowserRoute(t *testing.T) {
	for _, test := range []struct {
		name        string
		backendCode int
		routeCode   int
		available   bool
	}{
		{name: "healthy backend but missing Traefik router", backendCode: 200, routeCode: 404},
		{name: "healthy backend but unavailable proxy", backendCode: 200, routeCode: 503},
		{name: "unhealthy backend despite reachable proxy", backendCode: 503, routeCode: 200},
		{name: "both ready", backendCode: 200, routeCode: 200, available: true},
		{name: "authentication page ready", backendCode: 200, routeCode: 401, available: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.backendCode)
			}))
			defer backend.Close()
			route := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.routeCode)
			}))
			defer route.Close()
			definitions := []ServiceDefinition{{
				ID: "tool", Project: "tool", URL: route.URL, CheckURL: backend.URL,
			}}
			source := &stubContainerSource{containers: []DockerContainer{{
				State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
			}}}
			reader := newServiceReaderWithAvailability(definitions, source, NewHTTPAvailabilityChecker("", ""))

			listed := reader.Read(context.Background()).Services[0]
			gate, err := reader.CheckService(context.Background(), "tool")
			if err != nil {
				t.Fatal(err)
			}
			if listed.Available != test.available || gate.Available != test.available {
				t.Fatalf("list and gate must require both URLs: list=%#v gate=%#v", listed, gate)
			}
			if !test.available && listed.State != "degraded" {
				t.Fatalf("expected unavailable service to be degraded, got %q", listed.State)
			}
		})
	}
}

func TestServiceReaderWaitsForRoutePropagation(t *testing.T) {
	var routeCode atomic.Int32
	routeCode.Store(http.StatusNotFound)
	route := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(routeCode.Load()))
	}))
	defer route.Close()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	definitions := []ServiceDefinition{{ID: "tool", Project: "tool", URL: route.URL, CheckURL: backend.URL}}
	source := &stubContainerSource{containers: []DockerContainer{{
		State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	reader := newServiceReaderWithAvailability(definitions, source, NewHTTPAvailabilityChecker("", ""))

	for _, code := range []int{404, 503, 200, 404} {
		routeCode.Store(int32(code))
		result, err := reader.CheckService(context.Background(), "tool")
		if err != nil {
			t.Fatal(err)
		}
		if result.Available != (code == http.StatusOK) {
			t.Fatalf("route HTTP %d: unexpected availability %#v", code, result)
		}
	}
}

func TestCheckServiceReportsStoppedWithoutProbing(t *testing.T) {
	definitions := []ServiceDefinition{{ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example"}}
	source := &stubContainerSource{containers: []DockerContainer{{
		State: "exited", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}
	checker := &recordingAvailabilityChecker{available: true}
	result, err := newServiceReaderWithAvailability(definitions, source, checker).CheckService(context.Background(), "tool")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "stopped" || result.Available || checker.calls.Load() != 0 {
		t.Fatalf("stopped service must not be probed: calls=%d result=%#v", checker.calls.Load(), result)
	}
}

func TestCheckServiceProbesWhenDockerIsUnavailable(t *testing.T) {
	definitions := []ServiceDefinition{{ID: "tool", Name: "Tool", Project: "tool", URL: "https://tool.example"}}
	checker := &recordingAvailabilityChecker{available: true}
	reader := newServiceReaderWithAvailability(definitions, &stubContainerSource{err: errors.New("offline")}, checker)
	result, err := reader.CheckService(context.Background(), "tool")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "unknown" || !result.Available {
		t.Fatalf("expected route check despite unknown Docker state: %#v", result)
	}
}

func TestServiceReaderProbesBackendAndRouteConcurrently(t *testing.T) {
	release := make(chan struct{})
	var inFlight atomic.Int32
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if inFlight.Add(1) == 2 {
			close(release)
		}
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	})
	backend := httptest.NewServer(slow)
	defer backend.Close()
	route := httptest.NewServer(slow)
	defer route.Close()
	definitions := []ServiceDefinition{{ID: "tool", Project: "tool", URL: route.URL, CheckURL: backend.URL}}
	source := &stubContainerSource{containers: []DockerContainer{{
		State: "running", Labels: map[string]string{composeProjectLabel: "tool"},
	}}}

	started := time.Now()
	result, err := newServiceReaderWithAvailability(definitions, source, NewHTTPAvailabilityChecker("", "")).CheckService(context.Background(), "tool")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || time.Since(started) > 2*time.Second {
		t.Fatalf("expected both probes in parallel: elapsed=%s result=%#v", time.Since(started), result)
	}
}

func TestServiceReaderChecksPartiallyRunningProjects(t *testing.T) {
	definitions := []ServiceDefinition{{ID: "app", Project: "app", URL: "https://app.example"}}
	source := &stubContainerSource{containers: []DockerContainer{
		{Names: []string{"/app"}, State: "running", Labels: map[string]string{composeProjectLabel: "app"}},
		{Names: []string{"/app-db"}, State: "exited", Labels: map[string]string{composeProjectLabel: "app"}},
	}}
	checker := &recordingAvailabilityChecker{available: false}
	service := newServiceReaderWithAvailability(definitions, source, checker).Read(context.Background()).Services[0]
	if checker.calls.Load() != 1 || service.Availability != "checked" || service.Status != "1 of 2 running" {
		t.Fatalf("partial project must be probed and keep its status: %#v", service)
	}
}

func TestServiceReaderDeduplicatesDiscoveredIDs(t *testing.T) {
	definitions := []ServiceDefinition{{ID: "discovered-tool", Name: "Tool", Project: "tool-config"}}
	source := &stubContainerSource{containers: []DockerContainer{
		{State: "running", Labels: map[string]string{composeProjectLabel: "foo_bar"}},
		{State: "running", Labels: map[string]string{composeProjectLabel: "foo-bar"}},
		{State: "running", Labels: map[string]string{composeProjectLabel: "tool"}},
	}}
	services := newServiceReader(definitions, source).Read(context.Background()).Services
	seen := map[string]bool{}
	for _, service := range services {
		if seen[service.ID] {
			t.Fatalf("duplicate service id %q in %#v", service.ID, services)
		}
		seen[service.ID] = true
	}
	if !seen["discovered-foo-bar"] || !seen["discovered-foo-bar-2"] || !seen["discovered-tool-2"] {
		t.Fatalf("unexpected discovered ids: %v", seen)
	}
}
