package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
)

// ForwardTrafficType defines the supported traffic forwarding modes.
type ForwardTrafficType string

// Supported traffic forwarding types.
const (
	ForwardTrafficTypeTCP          ForwardTrafficType = "tcp"           // Direct TCP forwarding
	ForwardTrafficTypeHTTP         ForwardTrafficType = "http"          // HTTP forwarding
	ForwardTrafficTypeHTTPS        ForwardTrafficType = "https"         // HTTPS forwarding
	ForwardTrafficTypeTailnetProxy ForwardTrafficType = "tailnet_proxy" // Tailnet proxy mode
)

// Common errors.
var (
	ErrTargetAddrInvalid = errors.New("target-addr is invalid")
	ErrListenPortInvalid = errors.New("listen-port is invalid")
	ErrMissingAuthKey    = errors.New("TS_AUTHKEY environment variable is required")
	ErrMissingTargetAddr = errors.New("TARGET_ADDR is required when not in proxy mode (or use -proxy-mode)")
)

// Config holds the application configuration.
type Config struct {
	// Tailscale configuration
	TSHostname     string `env:"TS_HOSTNAME" env-default:"railtail"`           // Hostname for the Tailscale node
	TSLoginServer  string `env:"TS_LOGIN_SERVER"`                              // Custom login server (e.g., Headscale)
	TSStateDirPath string `env:"TS_STATEDIR_PATH" env-default:"/tmp/railtail"` // Directory to store Tailscale state
	TSAuthKey      string `env:"TS_AUTHKEY"`                                   // Tailscale auth key

	// Network configuration
	ListenPort         string `env:"LISTEN_PORT" env-default:"8080"`          // Port to listen on
	TargetAddr         string `env:"TARGET_ADDR"`                             // Target address to forward traffic to
	ProxyMode          bool   `env:"PROXY_MODE" env-default:"false"`          // Enable Tailnet proxy mode
	InsecureSkipVerify bool   `env:"INSECURE_SKIP_VERIFY" env-default:"true"` // Skip TLS verification for HTTPS

	// Derived fields (not directly set from environment or flags)
	ForwardTrafficType ForwardTrafficType // Determined based on configuration
}

// LoadConfig loads configuration from environment variables and command-line flags.
// Environment variables are loaded first, then overridden by flags if provided.
// Returns the loaded config and any validation errors.
func LoadConfig() (*Config, []error) {

	// Initialize with environment variables and defaults
	cfg, envErrors := loadEnvironmentConfig()

	// Override with command-line flags
	parseFlags(cfg)

	// Determine the traffic type and validate configuration
	validationErrors := validateConfig(cfg)

	// Combine flagErrors from environment loading and validation
	var flagErrors []error
	flagErrors = append(flagErrors, envErrors...)
	flagErrors = append(flagErrors, validationErrors...)

	if len(flagErrors) > 0 {
		return nil, flagErrors
	}

	return cfg, nil
}

// loadEnvironmentConfig loads configuration from environment variables.
func loadEnvironmentConfig() (*Config, []error) {
	var cfg Config
	var environmentErrors []error

	err := cleanenv.ReadEnv(&cfg)
	if err != nil {
		environmentErrors = append(
			environmentErrors,
			fmt.Errorf("error reading environment config: %w", err),
		)
	}

	return &cfg, environmentErrors
}

// parseFlags defines and parses command-line flags, updating the provided config.
func parseFlags(cfg *Config) {

	// Define flags, using current cfg values as defaults
	flag.StringVar(
		&cfg.TSHostname,
		"ts-hostname",
		cfg.TSHostname,
		"Hostname to use for Tailscale.",
	)
	flag.StringVar(
		&cfg.ListenPort,
		"listen-port",
		cfg.ListenPort,
		"Port to listen on.",
	)
	flag.StringVar(
		&cfg.TargetAddr,
		"target-addr",
		cfg.TargetAddr,
		"Target Tailscale node address (e.g., 100.x.x.x:port or http://100.x.x.x:port).",
	)
	flag.BoolVar(
		&cfg.ProxyMode,
		"proxy-mode",
		cfg.ProxyMode,
		"Enable Tailnet Proxy mode. TARGET_ADDR is ignored if true.",
	)
	flag.StringVar(
		&cfg.TSLoginServer,
		"ts-login-server",
		cfg.TSLoginServer,
		"Headscale users: your Headscale URL.",
	)
	flag.StringVar(
		&cfg.TSStateDirPath,
		"ts-state-dir",
		cfg.TSStateDirPath,
		"Directory to store Tailscale state.",
	)
	flag.BoolVar(
		&cfg.InsecureSkipVerify,
		"insecure-skip-verify",
		cfg.InsecureSkipVerify,
		"Skip TLS certificate verification for HTTPS targets.",
	)
	// Note: TSAuthKey is intentionally not exposed as a flag for security reasons

	// Parse command-line flags
	flag.Parse()
}

// validateConfig performs validation checks on the configuration and determines
// the ForwardTrafficType based on the configuration.
func validateConfig(cfg *Config) []error {
	var errors []error

	// Validate required fields
	if cfg.TSAuthKey == "" {
		errors = append(errors, ErrMissingAuthKey)
	}

	// Determine ForwardTrafficType and validate accordingly
	if cfg.ProxyMode {
		cfg.ForwardTrafficType = ForwardTrafficTypeTailnetProxy
	} else if cfg.TargetAddr == "" {
		errors = append(errors, ErrMissingTargetAddr)
	} else {
		// Determine and validate the traffic type based on the target address
		errors = append(errors, determineAndValidateTrafficType(cfg)...)
	}

	// Validate listen port
	if err := validateListenPort(cfg.ListenPort); err != nil {
		errors = append(errors, err)
	}

	return errors
}

// determineAndValidateTrafficType determines the ForwardTrafficType from the TargetAddr
// and validates the address format accordingly.
func determineAndValidateTrafficType(cfg *Config) []error {
	var errors_ []error

	// Determine type based on protocol prefix
	protocol := ""
	parts := strings.SplitN(cfg.TargetAddr, "://", 2)
	if len(parts) > 1 {
		protocol = strings.ToLower(parts[0])
	}

	switch protocol {
	case "http":
		cfg.ForwardTrafficType = ForwardTrafficTypeHTTP

	case "https":
		cfg.ForwardTrafficType = ForwardTrafficTypeHTTPS

	default:
		cfg.ForwardTrafficType = ForwardTrafficTypeTCP
	}

	// Validate based on type
	if cfg.ForwardTrafficType == ForwardTrafficTypeHTTP || cfg.ForwardTrafficType == ForwardTrafficTypeHTTPS {
		if err := validateHTTPAddress(cfg.TargetAddr); err != nil {
			errors_ = append(errors_, err)
		}
	} else {
		if err := validateTCPAddress(cfg.TargetAddr); err != nil {
			errors_ = append(errors_, err)
		}
	}

	return errors_
}

// validateHTTPAddress validates that the given address is a valid HTTP(S) URL.
func validateHTTPAddress(addr string) error {
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTargetAddrInvalid, err)
	}

	if u.Host == "" {
		return fmt.Errorf("%w: missing host in URL (%s)", ErrTargetAddrInvalid, addr)
	}

	return nil
}

// validateTCPAddress validates that the given address is a valid TCP address (host:port).
func validateTCPAddress(addr string) error {
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w for TCP mode ('%s'): %w. Expected host:port",
			ErrTargetAddrInvalid, addr, err)
	}

	return nil
}

// validateListenPort validates that the listen port is a valid port number.
func validateListenPort(port string) error {
	if port == "" {
		return errors.New("LISTEN_PORT is required")
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrListenPortInvalid, port, err)
	}

	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("%w: %s: port must be between 1 and 65535",
			ErrListenPortInvalid, port)
	}

	return nil
}
