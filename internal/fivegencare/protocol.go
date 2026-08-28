// Package fivegencare implements the newline-delimited TLS control protocol
// used by the Motorola Nursery app. The wire formats in this package are
// reconstructed from version 2.1.16 of the arm64 app and runtime captures.
package fivegencare

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	DefaultHost   = "primary.moto.5gencare.com"
	DefaultPort   = 3388
	TargetPort    = 6667
	DeviceAPIPort = 2288
)

type Session struct {
	UserID       int64  `json:"user_id"`
	SessionToken string `json:"session_token"`
	MasterToken  string `json:"master_token,omitempty"`
	SessionID    string `json:"session_id"`
	Domain       string `json:"domain"`
}

type Device struct {
	ID          uint32 `json:"id"`
	UDID        string `json:"udid"`
	Model       string `json:"model"`
	Name        string `json:"name"`
	DeviceToken string `json:"device_token"`
	SID         string `json:"sid"`
	Country     string `json:"country"`
}

func command(name string, values ...string) ([]byte, error) {
	if strings.ContainsAny(name, " \r\n\t") || name == "" {
		return nil, errors.New("invalid command name")
	}
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, " \r\n\t") {
			return nil, errors.New("command values must be non-empty single fields")
		}
	}
	return []byte(name + " " + strings.Join(values, " ") + "\n"), nil
}

func OTPCommand(deviceUUID, email string) ([]byte, error) {
	return command("v3_otp", deviceUUID, "email", email, "6")
}

func LoginSetCommand(userID int64, deviceUUID, email, code string) ([]byte, error) {
	return command("v3_loginset", strconv.FormatInt(userID, 10), deviceUUID, "email", email, code)
}

func SessionCommand(session Session) ([]byte, error) {
	// App order is user id, token, session id (not the persisted field order).
	return command("v3_session", strconv.FormatInt(session.UserID, 10), session.SessionToken, session.SessionID)
}

func SecretCommand(secret string) ([]byte, error) { return command("secret", secret) }

func ParseLogin(line string) (Session, error) {
	f := strings.Fields(line)
	if len(f) < 6 || (f[0] != "v3_login" && f[0] != "v3_loginset" && f[0] != "v3_session") {
		return Session{}, fmt.Errorf("unexpected login response")
	}
	uid, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || uid <= 0 {
		return Session{}, fmt.Errorf("login failed with status %q", f[1])
	}
	return Session{UserID: uid, SessionToken: f[2], MasterToken: f[3], SessionID: f[4], Domain: f[5]}, nil
}

func ParseRedirect(line, commandName string) (string, bool) {
	f := strings.Fields(line)
	if len(f) >= 3 && f[0] == commandName && f[1] == "-6" {
		return f[2], true
	}
	return "", false
}

func ParseSecret(line string) (string, error) {
	f := strings.Fields(line)
	if len(f) < 3 || f[0] != "secret" || f[1] != "1" {
		return "", fmt.Errorf("secret negotiation failed")
	}
	return f[2], nil
}

func ParseDeviceList(line string) ([]Device, error) {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "v3_dlist" {
		return nil, errors.New("unexpected device-list response")
	}
	count, err := strconv.Atoi(f[1])
	if err != nil || count < 0 {
		return nil, fmt.Errorf("invalid device count %q", f[1])
	}
	if len(f) != 2+count*7 {
		return nil, fmt.Errorf("device-list has %d fields, want %d", len(f), 2+count*7)
	}
	devices := make([]Device, 0, count)
	for i := 0; i < count; i++ {
		p := f[2+i*7 : 2+(i+1)*7]
		id, err := strconv.ParseUint(p[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("device %d id: %w", i, err)
		}
		name, err := url.QueryUnescape(p[3])
		if err != nil {
			return nil, fmt.Errorf("device %d name: %w", i, err)
		}
		// Named fields on purpose: a positional literal would silently swap SID
		// and DeviceToken if the struct were ever reordered.
		devices = append(devices, Device{
			ID:          uint32(id),
			UDID:        p[1],
			Model:       p[2],
			Name:        name,
			DeviceToken: p[4],
			SID:         p[5],
			Country:     p[6],
		})
	}
	return devices, nil
}

// OwnerAccessToken reproduces sha1OwnerToken from the app.
func OwnerAccessToken(deviceToken string) string {
	sum := sha1.Sum([]byte(deviceToken + "5GenCare.com"))
	return hex.EncodeToString(sum[:])
}
