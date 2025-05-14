// Package config provides configuration loading and parsing for railtail.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rmonvfer/railtail/internal/config/parser"
)

type ForwardTrafficType string

const (
	ForwardTrafficTypeTCP          ForwardTrafficType = "tcp"
	ForwardTrafficTypeHTTP         ForwardTrafficType = "http"
	ForwardTrafficTypeHTTPS        ForwardTrafficType = "https"
	ForwardTrafficTypeTailnetProxy ForwardTrafficType = "tailnet_proxy"
)

var (
	// ErrTargetAddrInvalid is returned when the target address is not valid
	ErrTargetAddrInvalid = errors.New("target-addr is invalid")
	
	// ErrListenPortInvalid is returned when the listen port is not valid
	ErrListenPortInvalid = errors.New("listen-port is invalid")
)

type Config struct {
	TSHostname         string `flag:"ts-hostname" env:"TS_HOSTNAME" usage:"hostname to use for tailscale"`
	ListenPort         string `flag:"listen-port" env:"LISTEN_PORT" usage:"port to listen on"`
	TargetAddr         string `flag:"target-addr" env:"TARGET_ADDR" usage:"address:port of a tailscale node to send traffic to; omit to operate as a general tailnet proxy"`
	ProxyMode          bool   `flag:"proxy-mode" env:"PROXY_MODE" default:"false" usage:"operate as a general tailnet proxy without requiring a specific target address"`
	TSLoginServer      string `flag:"ts-login-server" env:"TS_LOGIN_SERVER" default:"" usage:"base url of the control server, If you are using Headscale for your control server, use your Headscale instance's URL"`
	TSStateDirPath     string `flag:"ts-state-dir" env:"TS_STATEDIR_PATH" default:"/tmp/railtail" usage:"tailscale state dir"`
	TSAuthKey          string `env:"TS_AUTHKEY,TS_AUTH_KEY" usage:"tailscale auth key"`
	InsecureSkipVerify bool   `flag:"insecure-skip-verify" env:"INSECURE_SKIP_VERIFY" default:"true" usage:"skip TLS certificate verification when connecting via HTTPS"`

	ForwardTrafficType ForwardTrafficType
}

func init() {
	// add a help flag purely for the usage message
	flag.Bool("help", false, "Show help message")

	// Only parse and print usage if -help is present in arguments
	if checkForFlag("help") {
		// Create temporary config just to register all flags for a usage message
		cfg := &Config{}

		parser.ParseFlags(cfg)

		flag.Usage()
		os.Exit(0)
	}
}

// LoadConfig loads the configuration from environment variables and command line flags.
// It returns the loaded configuration and any errors encountered during loading.
func LoadConfig() (*Config, []error) {
	cfg := &Config{}

	configErrors := parser.ParseConfig(cfg)
	
	// Check for proxy mode
	if cfg.ProxyMode {
		cfg.ForwardTrafficType = ForwardTrafficTypeTailnetProxy
	} else if cfg.TargetAddr == "" {
		// TARGET_ADDR is still required when not in proxy mode
		configErrors = append(configErrors, fmt.Errorf("TARGET_ADDR is required when not in proxy mode. Either specify a target address or set PROXY_MODE=true"))
	}

	// Validate target-addr if it's set to either be a valid URL with a port or a valid address:port
	if cfg.TargetAddr != "" && !cfg.ProxyMode {
		protocol := ""
		parts := strings.SplitN(cfg.TargetAddr, "://", 2)
		if len(parts) > 1 {
			protocol = parts[0]
		}

		switch protocol {
		case "https", "http":
			cfg.ForwardTrafficType = ForwardTrafficType(protocol)

			u, err := url.Parse(cfg.TargetAddr)
			if err != nil {
				configErrors = append(configErrors, fmt.Errorf("%w: %w", ErrTargetAddrInvalid, err))
			}

			// Check if the URL has a port only if the URL is valid
			if err == nil && u.Port() == "" {
				configErrors = append(configErrors, fmt.Errorf("%w: address %s: missing port in address", ErrTargetAddrInvalid, cfg.TargetAddr))
			}

		default:
			cfg.ForwardTrafficType = ForwardTrafficTypeTCP

			_, _, err := net.SplitHostPort(cfg.TargetAddr)
			if err != nil {
				configErrors = append(configErrors, fmt.Errorf("%w: %w", ErrTargetAddrInvalid, err))
			}
		}
	}
	
	// Validate listen port
	if cfg.ListenPort != "" {
		port, err := strconv.Atoi(cfg.ListenPort)
		if err != nil {
			configErrors = append(configErrors, fmt.Errorf("%w: %s: %w", ErrListenPortInvalid, cfg.ListenPort, err))
		} else if port < 1 || port > 65535 {
			configErrors = append(configErrors, fmt.Errorf("%w: %s: port number must be between 1 and 65535", ErrListenPortInvalid, cfg.ListenPort))
		}
	}

	if len(configErrors) > 0 {
		return nil, configErrors
	}

	return cfg, nil
}
