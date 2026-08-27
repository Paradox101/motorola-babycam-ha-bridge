package magic

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ConnectionModeWEB2 is the Magic connection mode carried both in the outbound
// discovery request and as the final field of the eight-field response. It was
// runtime-confirmed as 2 for the measured WEB2 session.
const ConnectionModeWEB2 = 2

// RelayStreamPort is the fixed TCP port the client opens toward the response
// stream host to send the RelayOpen frame. Native default 9901 (0x26ad),
// runtime-confirmed: the measured 9901 flow targeted exactly the response
// stream host.
const RelayStreamPort = 9901

// AppRequest is the plaintext discovery request sent as the first payload to
// the Magic control host (native default TCP/8800). Native format string
// "app %s %d %d %s\n" at 0x15324. Runtime-confirmed byte-for-byte:
//
//	app <magicUuid> <targetPort> 2 <sessionName>\n
type AppRequest struct {
	MagicUUID   string
	TargetPort  int
	Mode        int
	SessionName string
}

func (r AppRequest) MarshalText() ([]byte, error) {
	if err := validateIdentifier("magic UUID", r.MagicUUID, 999); err != nil {
		return nil, err
	}
	if r.TargetPort < 0 || r.TargetPort > 99999 {
		return nil, errors.New("target port out of range")
	}
	if r.Mode < 0 || r.Mode > 999 {
		return nil, errors.New("mode out of range")
	}
	if err := validateIdentifier("session name", r.SessionName, 9999); err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "app %s %d %d %s\n",
		r.MagicUUID, r.TargetPort, r.Mode, r.SessionName), nil
}

// AppResponse is the Magic control host's answer to an AppRequest. The native
// parser (FUN_00017cf0) accepts four forms by whitespace-field count; only the
// eight-field form drives a WEB2 relay session and is the one reconstructed
// here in full. Fields below are runtime-confirmed against the same session's
// relay-open frame and 9901/direct flows.
//
//	app <num> <streamHost> <controlHost> <targetPort> <directIP> <directPort> <mode>
//
// The response is newline-terminated on the wire; the terminator is not part of
// any field.
type AppResponse struct {
	ConnectionNumber int    // "num"; reused as RelayOpen.ConnectionNumber
	StreamHost       string // relay stream host; RelayStreamPort is opened toward it
	ControlHost      string // relay control hostname
	TargetPort       int    // echoed camera target port
	DirectIP         string // camera LAN endpoint for the tryDirect attempt
	DirectPort       int    // camera LAN endpoint port
	Mode             int    // final field; runtime-confirmed as the connection mode
}

func ParseAppResponse(data []byte) (AppResponse, error) {
	var result AppResponse
	text := strings.TrimRight(string(data), "\r\n")
	fields := strings.Split(text, " ")
	if len(fields) < 2 || !strings.EqualFold(fields[0], "app") {
		return result, errors.New("response must begin with the app keyword")
	}

	num, err := strconv.Atoi(fields[1])
	if err != nil {
		return result, fmt.Errorf("connection number: %w", err)
	}
	// The native parser requires num > 0 before accepting the response.
	if num <= 0 {
		return result, errors.New("connection number must be positive")
	}
	result.ConnectionNumber = num

	// Only the eight-field form is a fully reconstructed WEB2 relay response.
	// Shorter forms are acknowledged by the native parser but were not part of
	// the measured session, so they are rejected here rather than guessed.
	if len(fields) != 8 {
		return result, fmt.Errorf("unsupported response field count %d; only the eight-field WEB2 form is reconstructed", len(fields))
	}

	result.StreamHost = fields[2]
	result.ControlHost = fields[3]
	if result.TargetPort, err = parsePort(fields[4], "target port"); err != nil {
		return result, err
	}
	result.DirectIP = fields[5]
	if result.DirectPort, err = parsePort(fields[6], "direct port"); err != nil {
		return result, err
	}
	if result.Mode, err = strconv.Atoi(fields[7]); err != nil {
		return result, fmt.Errorf("mode: %w", err)
	}
	if err := validateHost("stream host", result.StreamHost); err != nil {
		return result, err
	}
	if err := validateHost("control host", result.ControlHost); err != nil {
		return result, err
	}
	if err := validateHost("direct IP", result.DirectIP); err != nil {
		return result, err
	}
	return result, nil
}

func parsePort(field, name string) (int, error) {
	value, err := strconv.Atoi(field)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if value < 0 || value > 99999 {
		return 0, fmt.Errorf("%s out of range", name)
	}
	return value, nil
}

func validateHost(name, value string) error {
	if value == "" || len(value) > 253 {
		return fmt.Errorf("%s length must be 1..253 bytes", name)
	}
	if strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r > '~' }) >= 0 {
		return fmt.Errorf("%s must contain printable non-whitespace ASCII", name)
	}
	return nil
}
