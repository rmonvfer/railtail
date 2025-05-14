// Package main provides a HTTP/TCP proxy for connecting Railway workloads to Tailscale nodes.
package main

import (
	"cmp"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rmonvfer/railtail/internal/config"
	"github.com/rmonvfer/railtail/internal/logger"

	"tailscale.com/tsnet"
)

func main() {
	cfg, errs := config.LoadConfig()
	if len(errs) > 0 {
		logger.StderrWithSource.Error("configuration error(s) found", logger.ErrorsAttr(errs...))
		os.Exit(1)
	}

	ts := &tsnet.Server{
		Hostname:     cfg.TSHostname,
		AuthKey:      cfg.TSAuthKey,
		RunWebClient: false,
		Ephemeral:    false,
		ControlURL:   cfg.TSLoginServer,
		UserLogf: func(format string, v ...any) {
			logger.Stdout.Info(fmt.Sprintf(format, v...))
		},
		Dir: filepath.Join(cfg.TSStateDirPath, "railtail"),
	}
	if err := ts.Start(); err != nil {
		logger.StderrWithSource.Error("failed to start tailscale network server", logger.ErrAttr(err))
		os.Exit(1)
	}

	defer func(ts *tsnet.Server) {
		err := ts.Close()
		if err != nil {
			logger.StderrWithSource.Error("failed to close tailscale network server", logger.ErrAttr(err))
		}
	}(ts)

	listenAddr := "[::]:" + cfg.ListenPort

	// Create the directory if it doesn't exist
	stateDir := filepath.Join(cfg.TSStateDirPath, "railtail")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		logger.StderrWithSource.Error("failed to create state directory", logger.ErrAttr(err))
		os.Exit(1)
	}

	logger.Stdout.Info("🚀 Starting railtail",
		slog.String("ts-hostname", cfg.TSHostname),
		slog.String("listen-addr", listenAddr),
		slog.String("target-addr", cfg.TargetAddr),
		slog.String("ts-login-server", cmp.Or(cfg.TSLoginServer, "using_default")),
		slog.String("ts-state-dir", stateDir),
		slog.Bool("insecure-skip-verify", cfg.InsecureSkipVerify),
	)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.StderrWithSource.Error("failed to start local listener", logger.ErrAttr(err))
		os.Exit(1)
	}

	// Get the HTTP client from Tailscale - needed for both HTTP and Tailnet proxy modes
	httpClient := ts.HTTPClient()
	
	// Safety check for nil transport
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		logger.StderrWithSource.Error("failed to get HTTP transport", 
			slog.String("err", "unexpected transport type"))
		os.Exit(1)
	}

	// Configure TLS settings based on user preferences
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	if cfg.ForwardTrafficType == config.ForwardTrafficTypeTailnetProxy {
		logger.Stdout.Info("running in Tailnet Proxy mode",
			slog.String("listen-addr", listenAddr),
			slog.Bool("proxy-mode", cfg.ProxyMode),
		)
		
		// Create a tailnet proxy handler
		tailnetProxy := NewTailnetProxy(httpClient, cfg.InsecureSkipVerify)
		
		// Setup the HTTP server
		server := http.Server{
			IdleTimeout:       60 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      60 * time.Second,
			MaxHeaderBytes:    1 << 20, // 1MB
			Handler:           tailnetProxy,
		}
		
		// Start serving requests
		if err := server.Serve(listener); err != nil {
			logger.StderrWithSource.Error("failed to start tailnet proxy server", logger.ErrAttr(err))
			os.Exit(1)
		}
	} else if cfg.ForwardTrafficType == config.ForwardTrafficTypeHTTP || cfg.ForwardTrafficType == config.ForwardTrafficTypeHTTPS {
		logger.Stdout.Info("running in HTTP/s proxy mode (http(s):// scheme detected in targetAddr)",
			slog.String("listen-addr", listenAddr),
			slog.String("target-addr", cfg.TargetAddr),
		)

		// Get the HTTP client from Tailscale
		httpClient := ts.HTTPClient()
		
		// Safety check for nil transport - this should never happen,
		// but we want to avoid panics if the library changes
		transport, ok := httpClient.Transport.(*http.Transport)
		if !ok {
			logger.StderrWithSource.Error("failed to get HTTP transport", 
				slog.String("err", "unexpected transport type"))
			os.Exit(1)
		}

		// Configure TLS settings based on user preferences
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}

		server := http.Server{
			IdleTimeout:       60 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwardingInfo := []any{
					slog.String("remote-addr", r.RemoteAddr),
					slog.String("target", cfg.TargetAddr),
				}

				logger.Stdout.Info("forwarding", forwardingInfo...)

				if err := fwdHttp(httpClient, cfg.TargetAddr, w, r); err != nil {
					logger.StderrWithSource.Error("failed to forward http request", append([]any{logger.ErrAttr(err)}, forwardingInfo...)...)
				}
			}),
		}

		if err := server.Serve(listener); err != nil {
			logger.StderrWithSource.Error("failed to start http server", logger.ErrAttr(err))
			os.Exit(1)
		}

	} else {
		// TCP mode
		logger.Stdout.Info("running in TCP tunnel mode (no HTTP scheme detected in targetAddr)",
			slog.String("listen-addr", listenAddr),
			slog.String("target-addr", cfg.TargetAddr),
		)

		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.StderrWithSource.Error("failed to accept connection", logger.ErrAttr(err))
				continue
			}

			forwardingInfo := []any{
				slog.String("local-addr", conn.LocalAddr().String()),
				slog.String("remote-addr", conn.RemoteAddr().String()),
				slog.String("target", cfg.TargetAddr),
			}

			logger.Stdout.Info("forwarding tcp connection", forwardingInfo...)

			// Use a separate variable to capture the connection for the goroutine
			go func(clientConn net.Conn, connInfo []any) {
				// We need to use a copy of the forwarding info to prevent race conditions
				// with the parent goroutine that's accepting connections
				connForwardingInfo := make([]any, len(connInfo))
				copy(connForwardingInfo, connInfo)
				
				// Set connection timeout
				if err := clientConn.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
					logger.StderrWithSource.Error("failed to set connection deadline",
						append([]any{logger.ErrAttr(err)}, connForwardingInfo...)...)
				}

				if err := fwdTCP(clientConn, ts, cfg.TargetAddr); err != nil {
					logger.StderrWithSource.Error("forwarding failed",
						append([]any{logger.ErrAttr(err)}, connForwardingInfo...)...)
				}
			}(conn, forwardingInfo)
		}
	}
}
