// Package magic implements only those Magic WEB2 protocol pieces that have
// been demonstrated by native-code analysis and runtime captures.
package magic

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const RelayOpenVersion2 = 2

// RelayOpen is the plaintext frame sent as the first TCP payload to a WEB2
// stream relay. Names reflect correlated runtime/native evidence. No relay
// response or crypto behavior is implied by this type.
type RelayOpen struct {
	Version          int
	ConnectionNumber int
	TargetPort       int
	MagicUUID        string
	SessionName      string
}

func (f RelayOpen) MarshalText() ([]byte, error) {
	if f.Version < 0 || f.Version > 999 {
		return nil, errors.New("version must fit three decimal digits")
	}
	if f.ConnectionNumber < 0 || f.ConnectionNumber > 999 {
		return nil, errors.New("connection number must fit three decimal digits")
	}
	if f.TargetPort < 0 || f.TargetPort > 99999 {
		return nil, errors.New("target port must fit five decimal digits")
	}
	if err := validateIdentifier("magic UUID", f.MagicUUID, 999); err != nil {
		return nil, err
	}
	if err := validateIdentifier("session name", f.SessionName, 9999); err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "v%03d %03d %05d %03d %s %04d %s",
		f.Version, f.ConnectionNumber, f.TargetPort, len(f.MagicUUID),
		f.MagicUUID, len(f.SessionName), f.SessionName), nil
}

func ParseRelayOpen(data []byte) (RelayOpen, error) {
	var result RelayOpen
	if len(data) < 24 || data[0] != 'v' {
		return result, errors.New("invalid relay-open prefix or truncated frame")
	}
	version, err := fixedDecimal(data, 1, 3, "version")
	if err != nil || data[4] != ' ' {
		return result, errors.New("invalid version field")
	}
	connection, err := fixedDecimal(data, 5, 3, "connection number")
	if err != nil || data[8] != ' ' {
		return result, errors.New("invalid connection-number field")
	}
	target, err := fixedDecimal(data, 9, 5, "target port")
	if err != nil || data[14] != ' ' {
		return result, errors.New("invalid target-port field")
	}
	magicLen, err := fixedDecimal(data, 15, 3, "magic UUID length")
	if err != nil || data[18] != ' ' {
		return result, errors.New("invalid magic-UUID length field")
	}
	magicStart, magicEnd := 19, 19+magicLen
	if magicEnd+6 > len(data) || data[magicEnd] != ' ' {
		return result, errors.New("truncated magic UUID")
	}
	sessionLen, err := fixedDecimal(data, magicEnd+1, 4, "session-name length")
	if err != nil || data[magicEnd+5] != ' ' {
		return result, errors.New("invalid session-name length field")
	}
	sessionStart, sessionEnd := magicEnd+6, magicEnd+6+sessionLen
	if sessionEnd != len(data) {
		return result, errors.New("session-name length does not match frame size")
	}
	result = RelayOpen{
		Version:          version,
		ConnectionNumber: connection,
		TargetPort:       target,
		MagicUUID:        string(data[magicStart:magicEnd]),
		SessionName:      string(data[sessionStart:sessionEnd]),
	}
	if err := validateIdentifier("magic UUID", result.MagicUUID, 999); err != nil {
		return RelayOpen{}, err
	}
	if err := validateIdentifier("session name", result.SessionName, 9999); err != nil {
		return RelayOpen{}, err
	}
	return result, nil
}

func fixedDecimal(data []byte, offset, width int, name string) (int, error) {
	if offset < 0 || width < 1 || offset+width > len(data) {
		return 0, fmt.Errorf("truncated %s", name)
	}
	for _, value := range data[offset : offset+width] {
		if value < '0' || value > '9' {
			return 0, fmt.Errorf("non-decimal %s", name)
		}
	}
	return strconv.Atoi(string(data[offset : offset+width]))
}

func validateIdentifier(name, value string, maximum int) error {
	if value == "" || len(value) > maximum {
		return fmt.Errorf("%s length must be 1..%d bytes", name, maximum)
	}
	if strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r > '~' }) >= 0 {
		return fmt.Errorf("%s must contain printable non-whitespace ASCII", name)
	}
	return nil
}
