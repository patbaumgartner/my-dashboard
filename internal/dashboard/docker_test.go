package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDockerClientUsesOnlyExpectedContainerEndpoints(t *testing.T) {
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		if request.Method == http.MethodGet {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{
				"Id":"container-id","Names":["/tool"],"Image":"tool:1",
				"State":"exited","Status":"Exited (0)",
				"Labels":{"com.docker.compose.project":"tool"}
			}]`))
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewDockerClient(server.URL)
	if err != nil {
		t.Fatalf("new Docker client: %v", err)
	}
	containers, err := client.ListContainers(context.Background())
	if err != nil || len(containers) != 1 || containers[0].ID != "container-id" {
		t.Fatalf("unexpected container response: containers=%#v error=%v", containers, err)
	}
	operations := []struct {
		name      string
		operation func(context.Context, string) error
	}{
		{name: "start", operation: client.StartContainer},
		{name: "stop", operation: client.StopContainer},
		{name: "restart", operation: client.RestartContainer},
	}
	for _, operation := range operations {
		if err := operation.operation(context.Background(), "container-id"); err != nil {
			t.Fatalf("%s container: %v", operation.name, err)
		}
	}

	expected := []string{
		"GET /containers/json?all=1",
		"POST /containers/container-id/start",
		"POST /containers/container-id/stop?t=10",
		"POST /containers/container-id/restart?t=10",
	}
	if !reflect.DeepEqual(requests, expected) {
		t.Fatalf("unexpected Docker requests:\nwant: %s\ngot:  %s", fmt.Sprint(expected), fmt.Sprint(requests))
	}
}

func TestDockerClientRejectsUnsafeContainerID(t *testing.T) {
	client, err := NewDockerClient("http://docker-gateway:2375")
	if err != nil {
		t.Fatalf("new Docker client: %v", err)
	}
	if err := client.StartContainer(context.Background(), "../other"); err == nil {
		t.Fatal("expected unsafe container id to be rejected")
	}
}
