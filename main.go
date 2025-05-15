package main

import (
	"cmp"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rmonvfer/railtail/internal/logger"

	"tailscale.com/tsnet"
)

func main() {
	cfg, errs := LoadConfig()
	if len(errs) > 0 {
		logger.StderrWithSource.Error().Strs("errors", logger.ErrorsValue(errs...)).Msg("configuration error(s) found")
		os.Exit(1)
	}

	ts := &tsnet.Server{
		Hostname:     cfg.TSHostname,
		AuthKey:      cfg.TSAuthKey,
		RunWebClient: false,
		Ephemeral:    false,
		ControlURL:   cfg.TSLoginServer,
		UserLogf: func(format string, v ...any) {
			logger.Stdout.Info().Msgf(format, v...)
		},
		Dir: filepath.Join(cfg.TSStateDirPath, "railtail"),
	}
	if err := ts.Start(); err != nil {
		logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to start tailscale network server")
		os.Exit(1)
	}

	defer func(ts *tsnet.Server) {
		err := ts.Close()
		if err != nil {
			logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to close tailscale network server")
		}
	}(ts)

	listenAddr := "[::]:" + cfg.ListenPort

	// Create the directory if it doesn't exist
	stateDir := filepath.Join(cfg.TSStateDirPath, "railtail")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to create state directory")
		os.Exit(1)
	}

	logger.Stdout.Info().
		Str("ts-hostname", cfg.TSHostname).
		Str("listen-addr", listenAddr).
		Str("target-addr", cfg.TargetAddr).
		Str("ts-login-server", cmp.Or(cfg.TSLoginServer, "using_default")).
		Str("ts-state-dir", stateDir).
		Bool("insecure-skip-verify", cfg.InsecureSkipVerify).
		Msg("🚀 Starting railtail")

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to start local listener")
		os.Exit(1)
	}

	// Get the HTTP client from Tailscale - needed for both HTTP and Tailnet proxy modes
	httpClient := ts.HTTPClient()

	// Safety check for nil transport
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		logger.StderrWithSource.Error().Str("err", "unexpected transport type").Msg("failed to get HTTP transport")
		os.Exit(1)
	}

	// Configure TLS settings based on user preferences
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	if cfg.ForwardTrafficType == ForwardTrafficTypeTailnetProxy {
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Bool("proxy-mode", cfg.ProxyMode).
			Msg("running in Tailnet Proxy mode")

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
			logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to start tailnet proxy server")
			os.Exit(1)
		}
	} else if cfg.ForwardTrafficType == ForwardTrafficTypeHTTP || cfg.ForwardTrafficType == ForwardTrafficTypeHTTPS {
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Str("target-addr", cfg.TargetAddr).
			Msg("running in HTTP/s proxy mode (http(s):// scheme detected in targetAddr)")

		// Get the HTTP client from Tailscale
		httpClient := ts.HTTPClient()

		// Safety check for nil transport - this should never happen,
		// but we want to avoid panics if the library changes
		transport, ok := httpClient.Transport.(*http.Transport)
		if !ok {
			logger.StderrWithSource.Error().
				Str("err", "unexpected transport type").
				Msg("failed to get HTTP transport")
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
				logger.Stdout.Info().
					Str("remote-addr", r.RemoteAddr).
					Str("target", cfg.TargetAddr).
					Msg("forwarding")

				if err := fwdHttp(httpClient, cfg.TargetAddr, w, r); err != nil {
					logger.StderrWithSource.Error().
						Str(logger.ErrAttr(err), logger.ErrValue(err)).
						Str("remote-addr", r.RemoteAddr).
						Str("target", cfg.TargetAddr).
						Msg("failed to forward http request")
				}
			}),
		}

		if err := server.Serve(listener); err != nil {
			logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to start http server")
			os.Exit(1)
		}

	} else {
		// TCP mode
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Str("target-addr", cfg.TargetAddr).
			Msg("running in TCP tunnel mode (no HTTP scheme detected in targetAddr)")

		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.StderrWithSource.Error().Str(logger.ErrAttr(err), logger.ErrValue(err)).Msg("failed to accept connection")
				continue
			}

			logEvent := logger.Stdout.Info().
				Str("local-addr", conn.LocalAddr().String()).
				Str("remote-addr", conn.RemoteAddr().String()).
				Str("target", cfg.TargetAddr)

			logEvent.Msg("forwarding tcp connection")

			// Use a separate variable to capture the connection for the goroutine
			go func(clientConn net.Conn) {
				// Set connection timeout
				if err := clientConn.SetDeadline(time.Now().Add(5 * time.Minute)); err != nil {
					logger.StderrWithSource.Error().
						Str(logger.ErrAttr(err), logger.ErrValue(err)).
						Str("local-addr", clientConn.LocalAddr().String()).
						Str("remote-addr", clientConn.RemoteAddr().String()).
						Str("target", cfg.TargetAddr).
						Msg("failed to set connection deadline")
				}

				if err := fwdTCP(clientConn, ts, cfg.TargetAddr); err != nil {
					logger.StderrWithSource.Error().
						Str(logger.ErrAttr(err), logger.ErrValue(err)).
						Str("local-addr", clientConn.LocalAddr().String()).
						Str("remote-addr", clientConn.RemoteAddr().String()).
						Str("target", cfg.TargetAddr).
						Msg("forwarding failed")
				}
			}(conn)
		}
	}
}
