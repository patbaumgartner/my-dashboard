package dashboard

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const actionConfirmationHeader = "X-MyDashboard-Action"

type APIHandler struct {
	reader     *ServiceReader
	controller *ServiceController
	logger     *slog.Logger
}

func NewAPIHandler(reader *ServiceReader, controller *ServiceController, logger *slog.Logger) *APIHandler {
	return &APIHandler{reader: reader, controller: controller, logger: logger}
}

func (handler *APIHandler) Action(response http.ResponseWriter, request *http.Request) {
	if !validActionRequest(request) {
		http.Error(response, "forbidden", http.StatusForbidden)
		return
	}

	serviceID := request.PathValue("id")
	action := ServiceAction(request.PathValue("action"))
	result, err := handler.controller.Execute(request.Context(), serviceID, action)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, ErrServiceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrActionForbidden):
			status = http.StatusForbidden
		case errors.Is(err, ErrServiceBusy), errors.Is(err, ErrNoContainers), errors.Is(err, ErrComposeRequired):
			status = http.StatusConflict
		}
		handler.logger.Warn("service action rejected", "service", serviceID, "action", action, "status", status, "error", err)
		http.Error(response, actionErrorMessage(err), status)
		return
	}

	handler.logger.Info("service action completed", "service", result.ServiceID, "action", result.Action, "container", result.Container)
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(response).Encode(result); err != nil {
		handler.logger.Error("encode action response", "error", err)
	}
}

func (handler *APIHandler) Services(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := handler.reader.Read(request.Context())
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(response).Encode(result); err != nil {
		handler.logger.Error("encode services response", "error", err)
	}
}

func (handler *APIHandler) Availability(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	serviceID := request.PathValue("id")
	result, err := handler.reader.CheckService(request.Context(), serviceID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ErrServiceNotFound) {
			status = http.StatusNotFound
		}
		handler.logger.Warn("availability check rejected", "service", serviceID, "status", status, "error", err)
		message := "Availability check failed"
		if status == http.StatusNotFound {
			message = "Service not found"
		}
		http.Error(response, message, status)
		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(response).Encode(result); err != nil {
		handler.logger.Error("encode availability response", "error", err)
	}
}

func Health(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}

func NewRouter(reader *ServiceReader, controller *ServiceController, staticFiles fs.FS, logger *slog.Logger) http.Handler {
	api := NewAPIHandler(reader, controller, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/services", api.Services)
	mux.HandleFunc("/mydashboard/api/services", api.Services)
	mux.HandleFunc("POST /api/services/{id}/actions/{action}", api.Action)
	mux.HandleFunc("POST /mydashboard/api/services/{id}/actions/{action}", api.Action)
	mux.HandleFunc("/api/services/{id}/availability", api.Availability)
	mux.HandleFunc("/mydashboard/api/services/{id}/availability", api.Availability)
	mux.HandleFunc("/healthz", Health)
	mux.HandleFunc("/mydashboard/healthz", Health)
	mux.HandleFunc("/mydashboard", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.Redirect(response, request, "/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/mydashboard/", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.Redirect(response, request, "/", http.StatusTemporaryRedirect)
	})
	mux.Handle("/", spaHandler(staticFiles))
	return securityHeaders(mux)
}

// actionErrorMessage maps controller errors to fixed client messages so Docker
// response bodies and internal endpoints stay in the server log.
func actionErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrServiceNotFound):
		return "Service not found"
	case errors.Is(err, ErrActionForbidden):
		return "This action is not allowed for the service in its current state"
	case errors.Is(err, ErrServiceBusy):
		return "Another action for this service is still running"
	case errors.Is(err, ErrNoContainers):
		return "The service has no container yet"
	case errors.Is(err, ErrComposeRequired):
		return "Multi-container projects cannot be controlled here"
	default:
		return "Docker did not complete the action"
	}
}

func validActionRequest(request *http.Request) bool {
	if request.Header.Get(actionConfirmationHeader) != "confirm" {
		return false
	}
	if fetchSite := request.Header.Get("Sec-Fetch-Site"); fetchSite != "" && fetchSite != "same-origin" {
		return false
	}

	origin, err := url.Parse(request.Header.Get("Origin"))
	if err != nil || origin.Host == "" || origin.Host != request.Host {
		return false
	}
	expectedScheme := "http"
	if request.TLS != nil {
		expectedScheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded != "" {
		expectedScheme = forwarded
	}
	return origin.Scheme == expectedScheme
}

func spaHandler(staticFiles fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFiles))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestedPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requestedPath == "." || requestedPath == "" {
			requestedPath = "index.html"
		}
		info, err := fs.Stat(staticFiles, requestedPath)
		missing := err != nil || info.IsDir()
		// Hashed build assets must never fall back to index.html: a stale page
		// would receive HTML for its script and render blank under nosniff.
		if strings.HasPrefix(requestedPath, "assets/") {
			if missing {
				http.NotFound(response, request)
				return
			}
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(response, request)
			return
		}
		// Directories and client routes fall through to the SPA entry point
		// rather than letting http.FileServer render an index of the build output.
		if missing || requestedPath == "index.html" {
			request.URL.Path = "/"
			response.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(response, request)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; font-src 'self' data:; base-uri 'self'; form-action 'none'; frame-ancestors 'none'")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(response, request)
	})
}

func Server(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}
