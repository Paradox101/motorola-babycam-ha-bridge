// Command magicbridge runs a local TCP-to-Magic-WEB2 bridge so a standard RTSP
// client (go2rtc, ffmpeg, VLC) can reach a Motorola/5GenCare camera through the
// proven Magic relay transport without the Android app.
//
// It never prints secret inputs. Credentials come from a local JSON file or
// environment variables. This tool reconstructs only the Magic transport layer;
// it does not perform the 5GenCare-side authorization that makes the camera
// attach to the relay session (see docs/missing-protocol-pieces.md). Without
// that authorization the relay accepts the session but no camera bytes flow.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
)

// fileCreds mirrors the credential JSON shape shared with cmd/tunnelcheck.
type fileCreds struct {
	DeviceID    uint32 `json:"device_id"`
	SID         string `json:"sid"`
	DeviceToken string `json:"device_token"`
	ControlHost string `json:"control_host"`
	ControlPort int    `json:"control_port"`
	TargetPort  int    `json:"target_port"`
}

func main() {
	credsPath := flag.String("creds", "", "path to a credentials JSON file (device_id, sid, device_token, control_host, target_port); overrides env")
	listen := flag.String("listen", "0.0.0.0:8554", "local TCP address to expose the camera RTSP on")
	flag.Parse()

	logger := log.New(os.Stderr, "magicbridge: ", log.LstdFlags)

	creds, err := loadCredentials(*credsPath)
	if err != nil {
		logger.Fatalf("load credentials: %v", err)
	}

	srv, err := bridge.New(bridge.Config{
		Credentials: creds,
		ListenAddr:  *listen,
		Logf:        logger.Printf,
	})
	if err != nil {
		logger.Fatalf("configure bridge: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Printf("starting; camera RTSP will be reachable at rtsp://%s/ once a 5GenCare-authorized session exists", *listen)
	logger.Printf("note: this bridge does not perform 5GenCare authorization; the camera attaches only when an authorized control session has signaled it")

	if err := srv.Serve(ctx); err != nil {
		logger.Fatalf("serve: %v", err)
	}
	logger.Printf("stopped")
}

// loadCredentials reads from the JSON file when given, otherwise from
// environment variables. Missing required fields fail fast in bridge.New.
func loadCredentials(path string) (bridge.Credentials, error) {
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return bridge.Credentials{}, err
		}
		var c fileCreds
		if err := json.Unmarshal(raw, &c); err != nil {
			return bridge.Credentials{}, fmt.Errorf("parse creds json: %w", err)
		}
		return bridge.Credentials{
			DeviceID:    c.DeviceID,
			SID:         c.SID,
			DeviceToken: c.DeviceToken,
			ControlHost: c.ControlHost,
			ControlPort: c.ControlPort,
			TargetPort:  c.TargetPort,
		}, nil
	}

	deviceID, err := envUint32("MAGIC_DEVICE_ID")
	if err != nil {
		return bridge.Credentials{}, err
	}
	controlPort, err := envIntOptional("MAGIC_CONTROL_PORT")
	if err != nil {
		return bridge.Credentials{}, err
	}
	targetPort, err := envIntOptional("MAGIC_TARGET_PORT")
	if err != nil {
		return bridge.Credentials{}, err
	}
	return bridge.Credentials{
		DeviceID:    deviceID,
		SID:         os.Getenv("MAGIC_SID"),
		DeviceToken: os.Getenv("MAGIC_DEVICE_TOKEN"),
		ControlHost: os.Getenv("MAGIC_CONTROL_HOST"),
		ControlPort: controlPort,
		TargetPort:  targetPort,
	}, nil
}

func envUint32(name string) (uint32, error) {
	v := os.Getenv(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(v, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return uint32(n), nil
}

func envIntOptional(name string) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}
