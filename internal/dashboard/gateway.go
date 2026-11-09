package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type DockerGateway struct {
	operator        ContainerOperator
	allowedProjects map[string]struct{}
	logger          *slog.Logger
}

func NewDockerGateway(
	definitions []ServiceDefinition,
	operator ContainerOperator,
	logger *slog.Logger,
) *DockerGateway {
	allowedProjects := make(map[string]struct{})
	for _, definition := range definitions {
		if !definition.Hidden && definition.Control == ControlPower {
			allowedProjects[definition.Project] = struct{}{}
		}
	}
	return &DockerGateway{
		operator:        operator,
		allowedProjects: allowedProjects,
		logger:          logger,
	}
}

func (gateway *DockerGateway) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", gateway.health)
	mux.HandleFunc("GET /containers/json", gateway.listContainers)
	mux.HandleFunc("POST /containers/{id}/{action}", gateway.containerAction)
	return mux
}

func (gateway *DockerGateway) health(response http.ResponseWriter, request *http.Request) {
	if _, err := gateway.operator.ListContainers(request.Context()); err != nil {
		http.Error(response, "Docker unavailable", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}

func (gateway *DockerGateway) listContainers(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "all=1" {
		http.Error(response, "all=1 is required", http.StatusBadRequest)
		return
	}
	containers, err := gateway.operator.ListContainers(request.Context())
	if err != nil {
		http.Error(response, "Docker unavailable", http.StatusBadGateway)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(response).Encode(projectContainers(containers)); err != nil {
		gateway.logger.Error("encode gateway container response", "error", err)
	}
}

// projectContainers drops every field the dashboard does not read. Docker labels
// carry credential material such as Traefik basicAuth hashes, so only the Compose
// project label crosses the socket boundary.
func projectContainers(containers []DockerContainer) []DockerContainer {
	projected := make([]DockerContainer, 0, len(containers))
	for _, container := range containers {
		minimal := DockerContainer{
			ID:     container.ID,
			Names:  container.Names,
			Image:  container.Image,
			State:  container.State,
			Status: container.Status,
		}
		if project, ok := container.Labels[composeProjectLabel]; ok {
			minimal.Labels = map[string]string{composeProjectLabel: project}
		}
		projected = append(projected, minimal)
	}
	return projected
}

func (gateway *DockerGateway) containerAction(response http.ResponseWriter, request *http.Request) {
	if request.ContentLength != 0 {
		http.Error(response, "request body is not allowed", http.StatusBadRequest)
		return
	}
	action := ServiceAction(request.PathValue("action"))
	if !validAction(action) {
		http.Error(response, "action is not allowed", http.StatusForbidden)
		return
	}
	containerID := request.PathValue("id")
	if strings.ContainsAny(containerID, "/?#") || containerID == "" {
		http.Error(response, "invalid container id", http.StatusBadRequest)
		return
	}

	containers, err := gateway.operator.ListContainers(request.Context())
	if err != nil {
		http.Error(response, "Docker unavailable", http.StatusBadGateway)
		return
	}
	var target *DockerContainer
	for index := range containers {
		if containers[index].ID == containerID {
			target = &containers[index]
			break
		}
	}
	if target == nil {
		http.Error(response, "container not found", http.StatusNotFound)
		return
	}
	project := target.Labels[composeProjectLabel]
	if _, allowed := gateway.allowedProjects[project]; !allowed {
		http.Error(response, "project is not controllable", http.StatusForbidden)
		return
	}

	switch action {
	case ActionStart:
		err = gateway.operator.StartContainer(request.Context(), containerID)
	case ActionStop:
		err = gateway.operator.StopContainer(request.Context(), containerID)
	case ActionRestart:
		err = gateway.operator.RestartContainer(request.Context(), containerID)
	}
	if err != nil {
		http.Error(response, "Docker operation failed", http.StatusBadGateway)
		return
	}
	gateway.logger.Info("Docker gateway action", "project", project, "container", containerName(*target), "action", action)
	response.WriteHeader(http.StatusNoContent)
}
