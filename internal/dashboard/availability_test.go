package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPAvailabilityCheckerClassifiesRouteResponses(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		available bool
	}{
		{name: "ok", status: http.StatusOK, available: true},
		{name: "redirect", status: http.StatusTemporaryRedirect, available: true},
		{name: "authentication required", status: http.StatusUnauthorized, available: true},
		{name: "forbidden", status: http.StatusForbidden, available: true},
		{name: "route missing", status: http.StatusNotFound, available: false},
		{name: "backend unavailable", status: http.StatusServiceUnavailable, available: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
			}))
			defer server.Close()

			checker := &HTTPAvailabilityChecker{client: server.Client()}
			available, _ := checker.Check(context.Background(), server.URL)
			if available != test.available {
				t.Fatalf("status %d: expected available=%t, got %t", test.status, test.available, available)
			}
		})
	}
}

func TestHTTPAvailabilityCheckerRejectsInvalidAndUnreachableURLs(t *testing.T) {
	checker := NewHTTPAvailabilityChecker("", "")
	for _, target := range []string{"", "file:///etc/passwd", "http://127.0.0.1:1"} {
		available, _ := checker.Check(context.Background(), target)
		if available {
			t.Fatalf("expected %q to be unavailable", target)
		}
	}
}

func TestHTTPAvailabilityCheckerChecksRedirectDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/not-ready", http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	available, note := NewHTTPAvailabilityChecker("", "").Check(context.Background(), server.URL)
	if available || note != "Route not found" {
		t.Fatalf("redirect to missing route must not be ready: available=%t note=%q", available, note)
	}
}
