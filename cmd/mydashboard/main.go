package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/patbaumgartner/my-dashboard/internal/dashboard"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "healthcheck":
			runHealthcheck("http://127.0.0.1:8080/healthz")
			return
		case "gateway-healthcheck":
			runHealthcheck("http://127.0.0.1:2375/healthz")
			return
		case "version":
			fmt.Println(version)
			return
		case "gateway":
			runGateway()
			return
		}
	}
	runDashboard()
}

func runDashboard() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manifestPath := environment("MYDASHBOARD_MANIFEST", "/app/config/services.json")
	dockerEndpoint := environment("MYDASHBOARD_DOCKER_ENDPOINT", "http://docker-gateway:2375")
	staticPath := environment("MYDASHBOARD_STATIC_DIR", "/app/public")
	address := environment("MYDASHBOARD_ADDRESS", ":8080")

	manifest, err := dashboard.LoadManifestFile(manifestPath)
	if err != nil {
		logger.Error("load manifest", "error", err)
		os.Exit(1)
	}
	dockerClient, err := dashboard.NewDockerClient(dockerEndpoint)
	if err != nil {
		logger.Error("configure Docker client", "error", err)
		os.Exit(1)
	}
	staticFiles := os.DirFS(staticPath)

	availabilityChecker := dashboard.NewHTTPAvailabilityChecker("", "")
	reader := dashboard.NewServiceReader(manifest, dockerClient, availabilityChecker)
	controller := dashboard.NewServiceController(manifest.Services, dockerClient)
	server := dashboard.Server(address, dashboard.NewRouter(reader, controller, staticFiles, logger))
	serve(logger, server)
}

func runGateway() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	manifestPath := environment("MYDASHBOARD_MANIFEST", "/app/config/services.json")
	socketPath := environment("MYDASHBOARD_DOCKER_SOCKET", "/var/run/docker.sock")
	address := environment("MYDASHBOARD_GATEWAY_ADDRESS", ":2375")

	manifest, err := dashboard.LoadManifestFile(manifestPath)
	if err != nil {
		logger.Error("load gateway manifest", "error", err)
		os.Exit(1)
	}
	dockerClient, err := dashboard.NewDockerSocketClient(socketPath)
	if err != nil {
		logger.Error("configure Docker socket", "error", err)
		os.Exit(1)
	}

	gateway := dashboard.NewDockerGateway(manifest.Services, dockerClient, logger)
	server := dashboard.Server(address, gateway.Router())
	serve(logger, server)
}

func serve(logger *slog.Logger, server *http.Server) {
	go func() {
		logger.Info("myDashboard process listening", "address", server.Addr, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve myDashboard process", "error", err)
			os.Exit(1)
		}
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-shutdownContext.Done()

	context, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(context); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}

func runHealthcheck(endpoint string) {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
