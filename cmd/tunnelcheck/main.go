// Command tunnelcheck validates the reconstructed Magic WEB2 chain against the
// real relay using live credentials extracted from the app. It performs the
// app-discovery exchange, opens the relay stream, runs the device-token tunnel
// and sends a single RTSP OPTIONS request, printing the decoded response.
//
// It reads credentials from a local JSON file (never committed) and never
// prints the secret inputs. It is a research validation tool, not part of any
// redistributable client.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

type creds struct {
	DeviceID    uint32 `json:"device_id"`
	SID         string `json:"sid"`
	DeviceToken string `json:"device_token"`
	ControlHost string `json:"control_host"`
	TargetPort  int    `json:"target_port"`
}

func main() {
	credsPath := flag.String("creds", "runtime-logs/creds/creds.json", "path to the local credentials JSON")
	timeout := flag.Duration("timeout", 15*time.Second, "overall timeout")
	readonly := flag.Bool("readonly", false, "after relay-open, only read (no OPTIONS) to see if the relay holds the session open")
	predelay := flag.Duration("predelay", 0, "wait this long after relay-open before sending OPTIONS")
	flag.Parse()

	if err := run(*credsPath, *timeout, *readonly, *predelay); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run(credsPath string, timeout time.Duration, readonly bool, predelay time.Duration) error {
	raw, err := os.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("read creds: %w", err)
	}
	var c creds
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("parse creds: %w", err)
	}
	if c.TargetPort == 0 {
		c.TargetPort = 6667
	}

	magicUUID, err := magic.GenerateMagicUUID(c.DeviceID, c.SID, c.DeviceToken)
	if err != nil {
		return fmt.Errorf("derive magic uuid: %w", err)
	}
	sessionName := magic.NewSessionName()

	fmt.Printf("control host : %s:%d\n", c.ControlHost, magic.ControlPortDefault)
	fmt.Printf("device id    : %d\n", c.DeviceID)
	fmt.Printf("magic uuid   : %d bytes (derived)\n", len(magicUUID))
	fmt.Printf("session name : %s\n", sessionName)
	fmt.Printf("target port  : %d\n", c.TargetPort)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Println("\n[1] dialing control host and opening WEB2 relay...")
	tunnel, err := magic.Dial(ctx, magic.TunnelConfig{
		ControlHost: c.ControlHost,
		MagicUUID:   magicUUID,
		TargetPort:  c.TargetPort,
		SessionName: sessionName,
		DeviceToken: c.DeviceToken,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer tunnel.Close()

	r := tunnel.Response
	fmt.Printf("    app response: num=%d streamHost=%s controlHost=%s targetPort=%d mode=%d\n",
		r.ConnectionNumber, r.StreamHost, r.ControlHost, r.TargetPort, r.Mode)
	fmt.Printf("    relay stream : %s:%d\n", r.StreamHost, magic.RelayStreamPort)

	if readonly {
		fmt.Println("[2] readonly probe: waiting for the relay/camera to send or close...")
		tunnel.SetReadDeadline(time.Now().Add(timeout))
		buf := make([]byte, 4096)
		start := time.Now()
		n, err := tunnel.Read(buf)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			return fmt.Errorf("readonly after %s: %w", elapsed, err)
		}
		fmt.Printf("    relay sent %d bytes after %s (session held open)\n", n, elapsed)
		return nil
	}

	if predelay > 0 {
		fmt.Printf("    waiting %s after relay-open for camera to attach...\n", predelay)
		time.Sleep(predelay)
	}
	fmt.Println("[2] sending RTSP OPTIONS through the token tunnel...")
	request := fmt.Sprintf("OPTIONS rtsp://%s/owner/streaming RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: tunnelcheck\r\n\r\n", c.ControlHost)
	tunnel.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := tunnel.Write([]byte(request)); err != nil {
		return fmt.Errorf("write OPTIONS: %w", err)
	}

	fmt.Println("[3] reading decoded relay response...")
	tunnel.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, err := tunnel.Read(buf)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	fmt.Printf("\n--- decoded RTSP response (%d bytes) ---\n%s\n", n, buf[:n])
	fmt.Println("SUCCESS: byte-transparent Magic WEB2 tunnel established and camera responded.")
	return nil
}
