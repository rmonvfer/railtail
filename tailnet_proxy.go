// Package main provides a HTTP/TCP proxy for connecting Railway workloads to Tailscale nodes.
package main

import (
	"net/http"

	"github.com/rmonvfer/railtail/internal/logger"
	"log/slog"
)

// TailnetProxy is a general proxy for the tailnet that forwards requests to their
// tailscale destinations directly without requiring a specific target address.
type TailnetProxy struct {
	httpClient         *http.Client
	insecureSkipVerify bool
}

// NewTailnetProxy creates a new TailnetProxy with the given HTTP client
func NewTailnetProxy(httpClient *http.Client, insecureSkipVerify bool) *TailnetProxy {
	return &TailnetProxy{
		httpClient:         httpClient,
		insecureSkipVerify: insecureSkipVerify,
	}
}

// ServeHTTP implements the http.Handler interface
func (p *TailnetProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract target from the Host header
	targetHost := r.Host

	// Default to http:// scheme unless explicitly specified in the URL
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}

	// Construct the target URL
	targetURL := scheme + targetHost
	if targetHost == "" {
		http.Error(w, "No Host header provided", http.StatusBadRequest)
		logger.StderrWithSource.Error("no host header in request",
			slog.String("remote-addr", r.RemoteAddr))
		return
	}

	// Log the forwarding
	forwardingInfo := []any{
		slog.String("remote-addr", r.RemoteAddr),
		slog.String("host", targetHost),
		slog.String("target-url", targetURL),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	logger.Stdout.Info("tailnet proxy forwarding", forwardingInfo...)

	// Use the HTTP forwarding function to forward the request
	if err := fwdHttp(p.httpClient, targetURL, w, r); err != nil {
		logger.StderrWithSource.Error("failed to forward request",
			append([]any{logger.ErrAttr(err)}, forwardingInfo...)...)
	}
}
