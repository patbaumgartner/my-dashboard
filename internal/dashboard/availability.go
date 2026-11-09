package dashboard

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type AvailabilityChecker interface {
	Check(context.Context, string) (bool, string)
}

type HTTPAvailabilityChecker struct {
	client *http.Client
}

func NewHTTPAvailabilityChecker(overrideHost string, overrideIP string) *HTTPAvailabilityChecker {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err == nil && overrideIP != "" && strings.EqualFold(host, overrideHost) {
				address = net.JoinHostPort(overrideIP, port)
			}
			return dialer.DialContext(ctx, network, address)
		},
		TLSClientConfig:    &tls.Config{MinVersion: tls.VersionTLS12},
		DisableCompression: true,
		MaxIdleConns:       16,
		IdleConnTimeout:    30 * time.Second,
	}
	return &HTTPAvailabilityChecker{
		client: &http.Client{
			Transport: transport,
			Timeout:   4 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return http.ErrUseLastResponse
				}
				// Only follow redirects to the same host to avoid SSRF and
				// to make sure the final destination (not an intermediate
				// Traefik redirect) is what we consider "available".
				if req.URL.Host != via[0].URL.Host {
					return http.ErrUseLastResponse
				}
				return nil
			},
		},
	}
}

func (checker *HTTPAvailabilityChecker) Check(ctx context.Context, target string) (bool, string) {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false, "Invalid URL"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return false, "Invalid request"
	}
	request.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.1")
	request.Header.Set("User-Agent", "myDashboard/0.1 route-check")

	response, err := checker.client.Do(request)
	if err != nil {
		return false, "Route unreachable"
	}
	response.Body.Close()

	status := response.StatusCode
	if status == http.StatusNotFound {
		return false, "Route not found"
	}
	if status >= http.StatusInternalServerError {
		return false, fmt.Sprintf("HTTP %d", status)
	}
	return true, fmt.Sprintf("HTTP %d", status)
}
