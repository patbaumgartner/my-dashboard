package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type ContainerSource interface {
	ListContainers(context.Context) ([]DockerContainer, error)
}

type ContainerOperator interface {
	ContainerSource
	StartContainer(context.Context, string) error
	StopContainer(context.Context, string) error
	RestartContainer(context.Context, string) error
}

type DockerClient struct {
	baseURL string
	client  *http.Client
}

// dockerActionTimeout covers a stop or restart with Docker's 10 second grace
// period plus a slow container start, with headroom for the gateway hop.
// Container listing uses its own shorter deadline.
const dockerActionTimeout = 45 * time.Second

func NewDockerClient(endpoint string) (*DockerClient, error) {
	if err := validateDockerEndpoint(endpoint); err != nil {
		return nil, err
	}

	return &DockerClient{
		baseURL: strings.TrimRight(endpoint, "/"),
		client: &http.Client{
			Timeout: dockerActionTimeout,
		},
	}, nil
}

func NewDockerSocketClient(socketPath string) (*DockerClient, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, fmt.Errorf("Docker socket path must be absolute and clean")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
		MaxIdleConns:       4,
		IdleConnTimeout:    30 * time.Second,
	}
	return &DockerClient{
		baseURL: "http://docker",
		client:  &http.Client{Transport: transport, Timeout: dockerActionTimeout - 5*time.Second},
	}, nil
}

func validateDockerEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse Docker endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("Docker endpoint must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("Docker endpoint must contain a host and no embedded credentials")
	}
	return nil
}

func (client *DockerClient) ListContainers(ctx context.Context) ([]DockerContainer, error) {
	readContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(
		readContext,
		http.MethodGet,
		client.baseURL+"/containers/json?all=1",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create Docker request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query Docker proxy: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, fmt.Errorf("query Docker proxy: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var containers []DockerContainer
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&containers); err != nil {
		return nil, fmt.Errorf("decode Docker response: %w", err)
	}
	return containers, nil
}

func (client *DockerClient) StartContainer(ctx context.Context, containerID string) error {
	return client.containerAction(ctx, containerID, "start", "")
}

func (client *DockerClient) StopContainer(ctx context.Context, containerID string) error {
	return client.containerAction(ctx, containerID, "stop", "?t=10")
}

func (client *DockerClient) RestartContainer(ctx context.Context, containerID string) error {
	return client.containerAction(ctx, containerID, "restart", "?t=10")
}

func (client *DockerClient) containerAction(
	ctx context.Context,
	containerID string,
	action string,
	query string,
) error {
	if strings.TrimSpace(containerID) == "" || strings.ContainsAny(containerID, "/?#") {
		return fmt.Errorf("invalid Docker container id")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.baseURL+"/containers/"+containerID+"/"+action+query,
		nil,
	)
	if err != nil {
		return fmt.Errorf("create Docker %s request: %w", action, err)
	}

	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("Docker %s: %w", action, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("Docker %s: status %d: %s", action, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
