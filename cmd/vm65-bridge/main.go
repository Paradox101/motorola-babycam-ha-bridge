// Command vm65-bridge exposes a Motorola VM65 camera as a local RTSP-over-TCP
// endpoint, tunneling every byte through the reconstructed Magic WEB2 relay.
// Point any RTSP player, go2rtc or Home Assistant at the local address and it
// reaches the camera as if it were on the LAN.
//
// The bridge does not perform the 5GenCare control flow: that flow is the one
// part of the chain not reconstructable from an x86 host. Its outputs — device
// id, SID, device token and relay control host — are supplied to the bridge in
// a credentials file (see -creds). Obtaining and refreshing those credentials
// is out of scope for this tool; see docs/missing-protocol-pieces.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
)

// credsFile is the on-disk shape of the credentials. It mirrors the fields
// cmd/tunnelcheck already reads, so the same local file works for both.
type credsFile struct {
	DeviceID    uint32 `json:"device_id"`
	SID         string `json:"sid"`
	DeviceToken string `json:"device_token"`
	ControlHost string `json:"control_host"`
	ControlPort int    `json:"control_port"`
	TargetPort  int    `json:"target_port"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8554", "local address to expose the camera on (loopback recommended)")
	credsPath := flag.String("creds", "runtime-logs/creds/creds.json", "path to the local credentials JSON (never committed)")
	verbose := flag.Bool("v", false, "verbose (debug) logging")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(*listen, *credsPath, logger); err != nil {
		logger.Error("bridge exited with error", "err", err)
		os.Exit(1)
	}
}

func run(listen, credsPath string, logger *slog.Logger) error {
	creds, err := loadCreds(credsPath)
	if err != nil {
		return err
	}

	b, err := bridge.New(bridge.Config{
		ListenAddr:  listen,
		Credentials: creds,
		Logger:      logger,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting vm65-bridge", "listen", listen, "control_host", creds.ControlHost)
	return b.Serve(ctx)
}

func loadCreds(path string) (bridge.Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return bridge.Credentials{}, fmt.Errorf("read credentials %q: %w", path, err)
	}
	var f credsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return bridge.Credentials{}, fmt.Errorf("parse credentials %q: %w", path, err)
	}
	if f.SID == "" || f.DeviceToken == "" || f.ControlHost == "" {
		return bridge.Credentials{}, errors.New("credentials file must set sid, device_token and control_host")
	}
	return bridge.Credentials{
		DeviceID:    f.DeviceID,
		SID:         f.SID,
		DeviceToken: f.DeviceToken,
		ControlHost: f.ControlHost,
		ControlPort: f.ControlPort,
		TargetPort:  f.TargetPort,
	}, nil
}
