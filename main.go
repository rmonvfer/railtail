package main

import (
	"context"
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
		logger.StderrWithSource.Error().
			Strs("errors", logger.ErrorsValue(errs...)).
			Msg("configuration error(s) found")
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

	// Block until the node is fully online (30 s cap).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := ts.Up(ctx); err != nil { // Up waits, unlike Start.
		logger.StderrWithSource.Error().
			Str(logger.ErrAttr(err), logger.ErrValue(err)).
			Msg("failed to bring tailscale server up")
		os.Exit(1)
	}
	defer ts.Close()

	listenAddr := "[::]:" + cfg.ListenPort
	stateDir := filepath.Join(cfg.TSStateDirPath, "railtail")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		logger.StderrWithSource.Error().
			Str(logger.ErrAttr(err), logger.ErrValue(err)).
			Msg("failed to create state directory")
		os.Exit(1)
	}

	tsLoginServer := cfg.TSLoginServer
	if tsLoginServer == "" {
		tsLoginServer = "using_default"
	}
	logger.Stdout.Info().
		Str("ts-hostname", cfg.TSHostname).
		Str("listen-addr", listenAddr).
		Str("target-addr", cfg.TargetAddr).
		Str("ts-login-server", tsLoginServer).
		Str("ts-state-dir", stateDir).
		Bool("insecure-skip-verify", cfg.InsecureSkipVerify).
		Msg("🚀 Starting railtail")

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logger.StderrWithSource.Error().
			Str(logger.ErrAttr(err), logger.ErrValue(err)).
			Msg("failed to start local listener")
		os.Exit(1)
	}

	// Custom transport: tailnet dialer, no 5-min tsnet timeout.
	transport := &http.Transport{
		DialContext:     ts.Dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
		IdleConnTimeout: 90 * time.Second,
	}
	httpClient := &http.Client{Transport: transport}

	switch cfg.ForwardTrafficType {
	case ForwardTrafficTypeTailnetProxy:
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Bool("proxy-mode", cfg.ProxyMode).
			Msg("running in Tailnet Proxy mode")

		server := http.Server{
			IdleTimeout:       0,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      0,
			Handler:           NewTailnetProxy(httpClient, cfg.InsecureSkipVerify),
		}
		if err := server.Serve(listener); err != nil {
			logger.StderrWithSource.Error().
				Str(logger.ErrAttr(err), logger.ErrValue(err)).
				Msg("failed to start tailnet proxy server")
			os.Exit(1)
		}

	case ForwardTrafficTypeHTTP, ForwardTrafficTypeHTTPS:
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Str("target-addr", cfg.TargetAddr).
			Msg("running in HTTP/s proxy mode")

		server := http.Server{
			IdleTimeout:       0,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      0,
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
			logger.StderrWithSource.Error().
				Str(logger.ErrAttr(err), logger.ErrValue(err)).
				Msg("failed to start http server")
			os.Exit(1)
		}

	default: // TCP tunnel
		logger.Stdout.Info().
			Str("listen-addr", listenAddr).
			Str("target-addr", cfg.TargetAddr).
			Msg("running in TCP tunnel mode")

		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.StderrWithSource.Error().
					Str(logger.ErrAttr(err), logger.ErrValue(err)).
					Msg("failed to accept connection")
				continue
			}

			go func(c net.Conn) {
				_ = c.SetDeadline(time.Now().Add(5 * time.Minute))
				if err := fwdTCP(c, ts, cfg.TargetAddr); err != nil {
					logger.StderrWithSource.Error().
						Str(logger.ErrAttr(err), logger.ErrValue(err)).
						Str("remote-addr", c.RemoteAddr().String()).
						Msg("forwarding failed")
				}
			}(conn)
		}
	}
}
