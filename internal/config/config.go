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
	"strings"
	"time"
)

// MQTT contains optional Home Assistant discovery broker settings.
type MQTT struct {
	Host            string
	Port            int
	Username        string
	Password        string
	DiscoveryPrefix string
	// TLS dials the broker over TLS. Home Assistant's own broker service can
	// report a TLS listener (normally port 8883), and a plain TCP dial against
	// one connects and then never completes a handshake, which reaches the user
	// as entities that simply never appear.
	TLS bool
}

// Config is the validated long-running bridge configuration.
//
// SnapshotBase is the base URL under which Home Assistant reaches this
// process, not go2rtc: the bridge serves the still images itself so a cold
// camera cannot blow the ten-second budget Home Assistant allows an image
// entity. IngressAddr is where that endpoint, and the authenticated Web UI,
// listen. SnapshotBase therefore only decides whether an address is published
// over MQTT — the still images themselves are served whenever there is an
// ingress listener, because the Web UI shows them too.
type Config struct {
	ListenAddr              string
	CredentialsPath         string
	RegistryPath            string
	StatusAddr              string
	Verbose                 bool
	MQTT                    MQTT
	StreamURL               string
	SnapshotBase            string
	SnapshotTokenFile       string
	IngressAddr             string
	IngressTrustedCIDRs     []string
	Go2RTCRequired          bool
	Go2RTCURL               string
	ShutdownTimeout         time.Duration
	TemperaturePollInterval time.Duration
	// CameraRefreshInterval is how often a still frame is pushed to the MQTT
	// camera entity. Zero publishes no frames, and then no camera entity is
	// created: Home Assistant's camera platform has nothing to show without
	// them.
	CameraRefreshInterval time.Duration
	// AllowCredentialRefresh offers the Web UI's "refresh credentials" action.
	// The refresh itself belongs to the add-on's entrypoint, which owns the
	// account session, so the bridge only signals it; outside the add-on there
	// is nothing to signal and the action is not offered.
	AllowCredentialRefresh bool
	// AllowMediaRestart offers the Web UI's "restart media server" action,
	// which asks the bundled go2rtc to restart itself.
	AllowMediaRestart bool
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
	flags.BoolVar(&cfg.MQTT.TLS, "mqtt-tls", false, "connect to the MQTT broker over TLS")
	flags.StringVar(&cfg.MQTT.DiscoveryPrefix, "mqtt-discovery-prefix", "homeassistant", "MQTT discovery prefix")
	flags.StringVar(&cfg.StreamURL, "stream-url", "", "RTSP URL for MQTT discovery")
	flags.StringVar(&cfg.SnapshotBase, "snapshot-url-base", "", "public base URL of this bridge, used to build the MQTT snapshot image URL, e.g. http://local-vm65-bridge:8099")
	flags.StringVar(&cfg.SnapshotTokenFile, "snapshot-token-file", "", "file holding the snapshot URL token; created when missing")
	flags.StringVar(&cfg.IngressAddr, "ingress", "", "optional listen address for the authenticated Web UI and snapshot endpoints")
	var trustedCIDRs string
	flags.StringVar(&trustedCIDRs, "ingress-trusted-cidr", "", "comma-separated networks allowed to reach the Web UI and snapshots (default: the Supervisor network; \"any\" disables the check)")
	flags.BoolVar(&cfg.Go2RTCRequired, "go2rtc-required", false, "require a reachable go2rtc endpoint for readiness")
	flags.StringVar(&cfg.Go2RTCURL, "go2rtc-url", "http://127.0.0.1:1984/", "go2rtc readiness endpoint")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	flags.DurationVar(&cfg.TemperaturePollInterval, "temperature-poll-interval", 30*time.Second, "temperature polling interval when MQTT discovery is enabled")
	flags.DurationVar(&cfg.CameraRefreshInterval, "camera-refresh-interval", 0, "how often to push a still frame to the Home Assistant camera entity; zero publishes none")
	flags.BoolVar(&cfg.AllowCredentialRefresh, "allow-credential-refresh", false, "offer the Web UI action that asks the supervising entrypoint for a credential refresh")
	flags.BoolVar(&cfg.AllowMediaRestart, "allow-media-restart", false, "offer the Web UI action that restarts the bundled media server")
	if err := flags.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}

	cfg.IngressTrustedCIDRs = parseTrustedCIDRs(trustedCIDRs)
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
	if c.IngressAddr != "" {
		if err := validateAddress("ingress address", c.IngressAddr); err != nil {
			return err
		}
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if c.TemperaturePollInterval < 10*time.Second || c.TemperaturePollInterval > time.Hour {
		return errors.New("temperature poll interval must be between 10s and 1h")
	}
	// Every refresh makes go2rtc pull a frame from the camera over the relay,
	// so a very short interval is a standing cost on someone's connection.
	if c.CameraRefreshInterval != 0 && (c.CameraRefreshInterval < 5*time.Second || c.CameraRefreshInterval > time.Hour) {
		return errors.New("camera refresh interval must be zero or between 5s and 1h")
	}
	if c.Go2RTCRequired {
		parsed, err := url.Parse(c.Go2RTCURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("go2rtc URL must be an absolute http or https URL when required")
		}
	}
	// The snapshot base is checked outside the MQTT block: a base URL that is
	// wrong is worth reporting whether or not a broker is configured, and the
	// still images are served either way.
	if c.SnapshotBase != "" {
		snapshot, err := url.Parse(c.SnapshotBase)
		if err != nil || (snapshot.Scheme != "http" && snapshot.Scheme != "https") || snapshot.Host == "" {
			return errors.New("snapshot URL base must be an absolute http or https URL")
		}
		// The bridge serves the snapshots, so advertising a URL without
		// starting that listener would publish an address nothing answers on.
		if c.IngressAddr == "" {
			return errors.New("snapshot URL base requires an ingress listen address")
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

// StreamHost is the host part of the advertised RTSP URL: the address a browser
// or Home Assistant has to be able to reach. It is the value behind the most
// common failure this add-on has — a name that resolves nowhere useful — so the
// Web UI shows it.
func (c Config) StreamHost() string {
	if c.StreamURL == "" {
		return ""
	}
	parsed, err := url.Parse(c.StreamURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// parseTrustedCIDRs turns the flag value into the network list. An empty value
// keeps the default; "any" returns a non-nil empty slice, which disables the
// check for someone who deliberately fronts the add-on with something else.
func parseTrustedCIDRs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.EqualFold(value, "any") {
		return []string{}
	}
	parts := strings.Split(value, ",")
	networks := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			networks = append(networks, part)
		}
	}
	return networks
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
		"listen=%q status=%q credentials=%q registry=%q mqtt_host=%q mqtt_port=%d mqtt_tls=%t mqtt_user_set=%t mqtt_password_set=%t stream_url=%q snapshot_base=%q ingress=%q go2rtc_required=%t go2rtc_url=%q shutdown_timeout=%s temperature_poll_interval=%s camera_refresh_interval=%s",
		c.ListenAddr,
		c.StatusAddr,
		c.CredentialsPath,
		c.RegistryPath,
		c.MQTT.Host,
		c.MQTT.Port,
		c.MQTT.TLS,
		c.MQTT.Username != "",
		c.MQTT.Password != "",
		c.StreamURL,
		c.SnapshotBase,
		c.IngressAddr,
		c.Go2RTCRequired,
		c.Go2RTCURL,
		c.ShutdownTimeout,
		c.TemperaturePollInterval,
		c.CameraRefreshInterval,
	)
}
