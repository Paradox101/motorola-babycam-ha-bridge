// Package config parses and validates vm65-bridge runtime configuration.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"
)

// MQTT contains optional Home Assistant discovery broker settings.
type MQTT struct {
	Host            string
	Port            int
	Username        string
	Password        string
	DiscoveryPrefix string
}

// Config is the validated long-running bridge configuration.
type Config struct {
	ListenAddr      string
	CredentialsPath string
	RegistryPath    string
	StatusAddr      string
	Verbose         bool
	MQTT            MQTT
	StreamURL       string
	Go2RTCRequired  bool
	Go2RTCURL       string
	ShutdownTimeout time.Duration
}

// Load parses command-line arguments, applies secret environment overrides,
// and validates the result. lookupEnv is injectable so tests never mutate the
// process environment.
func Load(args []string, lookupEnv func(string) (string, bool)) (Config, error) {
	flags := flag.NewFlagSet("vm65-bridge", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var cfg Config
	var legacyMQTTPassword string
	flags.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:8554", "local bridge listen address")
	flags.StringVar(&cfg.CredentialsPath, "creds", "runtime-logs/creds/creds.json", "credentials JSON path")
	flags.StringVar(&cfg.RegistryPath, "registry", "", "optional multi-camera credentials registry")
	flags.StringVar(&cfg.StatusAddr, "status", "", "optional health/status listen address")
	flags.BoolVar(&cfg.Verbose, "v", false, "verbose logging")
	flags.StringVar(&cfg.MQTT.Host, "mqtt-host", "", "optional MQTT broker host")
	flags.IntVar(&cfg.MQTT.Port, "mqtt-port", 1883, "MQTT broker port")
	flags.StringVar(&cfg.MQTT.Username, "mqtt-username", "", "MQTT username")
	flags.StringVar(&legacyMQTTPassword, "mqtt-password", "", "deprecated: use VM65_MQTT_PASSWORD")
	flags.StringVar(&cfg.MQTT.DiscoveryPrefix, "mqtt-discovery-prefix", "homeassistant", "MQTT discovery prefix")
	flags.StringVar(&cfg.StreamURL, "stream-url", "", "RTSP URL for MQTT discovery")
	flags.BoolVar(&cfg.Go2RTCRequired, "go2rtc-required", false, "require a reachable go2rtc endpoint for readiness")
	flags.StringVar(&cfg.Go2RTCURL, "go2rtc-url", "http://127.0.0.1:1984/", "go2rtc readiness endpoint")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}

	cfg.MQTT.Password = legacyMQTTPassword
	if lookupEnv != nil {
		if password, ok := lookupEnv("VM65_MQTT_PASSWORD"); ok {
			cfg.MQTT.Password = password
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces settings that cannot be represented by individual flags.
func (c Config) Validate() error {
	if c.CredentialsPath == "" {
		return errors.New("credentials path is required")
	}
	if err := validateAddress("listen address", c.ListenAddr); err != nil {
		return err
	}
	if c.StatusAddr != "" {
		if err := validateAddress("status address", c.StatusAddr); err != nil {
			return err
		}
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if c.Go2RTCRequired {
		parsed, err := url.Parse(c.Go2RTCURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("go2rtc URL must be an absolute http or https URL when required")
		}
	}
	if c.MQTT.Host == "" {
		return nil
	}
	if c.MQTT.Port < 1 || c.MQTT.Port > 65535 {
		return errors.New("mqtt port must be between 1 and 65535")
	}
	if c.MQTT.DiscoveryPrefix == "" {
		return errors.New("mqtt discovery prefix is required when MQTT is enabled")
	}
	parsed, err := url.Parse(c.StreamURL)
	if err != nil || parsed.Scheme != "rtsp" || parsed.Host == "" || parsed.Path == "" {
		return errors.New("mqtt stream URL must be an absolute rtsp URL")
	}
	return nil
}

func validateAddress(name, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%s must use host:port format: %w", name, err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535", name)
	}
	return nil
}

// Redacted returns a log-safe configuration summary.
func (c Config) Redacted() string {
	return fmt.Sprintf(
		"listen=%q status=%q credentials=%q registry=%q mqtt_host=%q mqtt_port=%d mqtt_user_set=%t mqtt_password_set=%t stream_url=%q go2rtc_required=%t go2rtc_url=%q shutdown_timeout=%s",
		c.ListenAddr,
		c.StatusAddr,
		c.CredentialsPath,
		c.RegistryPath,
		c.MQTT.Host,
		c.MQTT.Port,
		c.MQTT.Username != "",
		c.MQTT.Password != "",
		c.StreamURL,
		c.Go2RTCRequired,
		c.Go2RTCURL,
		c.ShutdownTimeout,
	)
}
