package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8554" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.CredentialsPath != "runtime-logs/creds/creds.json" {
		t.Fatalf("CredentialsPath = %q", cfg.CredentialsPath)
	}
	if cfg.ShutdownTimeout.String() != "10s" {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
	if cfg.Go2RTCRequired {
		t.Fatal("go2rtc must be optional by default")
	}
}

func TestLoadValidatesTemperaturePollInterval(t *testing.T) {
	cfg, err := Load([]string{"-temperature-poll-interval", "45s"}, nil)
	if err != nil || cfg.TemperaturePollInterval != 45*time.Second {
		t.Fatalf("temperature interval = %s, error = %v", cfg.TemperaturePollInterval, err)
	}
	for _, value := range []string{"10s", "1h"} {
		if _, err := Load([]string{"-temperature-poll-interval", value}, nil); err != nil {
			t.Fatalf("valid temperature interval %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{"9s", "1h1s"} {
		if _, err := Load([]string{"-temperature-poll-interval", value}, nil); err == nil {
			t.Fatalf("invalid temperature interval %s accepted", value)
		}
	}
}

func TestLoadValidatesRequiredGo2RTCEndpoint(t *testing.T) {
	cfg, err := Load([]string{"-go2rtc-required", "-go2rtc-url", "http://127.0.0.1:1984/"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Go2RTCRequired || cfg.Go2RTCURL != "http://127.0.0.1:1984/" {
		t.Fatalf("go2rtc config = %#v", cfg)
	}
	if _, err := Load([]string{"-go2rtc-required", "-go2rtc-url", "not-a-url"}, nil); err == nil {
		t.Fatal("invalid required go2rtc URL was accepted")
	}
}

func TestLoadPrefersMQTTPasswordEnvironment(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "VM65_MQTT_PASSWORD" {
			return "environment-secret", true
		}
		return "", false
	}
	cfg, err := Load([]string{
		"-mqtt-host", "broker",
		"-mqtt-password", "legacy-secret",
		"-stream-url", "rtsp://camera.local:8555/nursery",
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MQTT.Password != "environment-secret" {
		t.Fatalf("password did not come from environment")
	}
	if strings.Contains(cfg.Redacted(), "environment-secret") {
		t.Fatal("redacted config contains MQTT password")
	}
}

func TestLoadRejectsInvalidMQTTConfigurationWithoutLeakingSecret(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "VM65_MQTT_PASSWORD" {
			return "never-log-this", true
		}
		return "", false
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"port", []string{"-mqtt-host", "broker", "-mqtt-port", "0", "-stream-url", "rtsp://host/camera"}, "mqtt port"},
		{"prefix", []string{"-mqtt-host", "broker", "-mqtt-discovery-prefix", "", "-stream-url", "rtsp://host/camera"}, "discovery prefix"},
		{"stream", []string{"-mqtt-host", "broker", "-stream-url", "http://host/camera"}, "stream URL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(test.args, lookup)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
			if strings.Contains(err.Error(), "never-log-this") {
				t.Fatal("validation error contains MQTT password")
			}
		})
	}
}
