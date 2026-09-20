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
// parser (FUN_00017cf0) accepts four forms by whitespace-field count. Two of
// them have come from the real relay and are reconstructed here:
//
//	app <num> <streamHost> <controlHost> <targetPort> <directIP> <directPort> <mode>
//	app <num> <streamHost> <controlHost>
//
// The eight-field form is the measured WEB2 session's; its fields are
// runtime-confirmed against the same session's relay-open frame and
// 9901/direct flows. The four-field form was seen in the field (2026-09-20)
// with num 1 — the first connection number after the camera registered with
// the relay again — and carries only the relay hosts: no LAN endpoint for a
// direct attempt, no echoed target port and no mode. The native parser fills
// the hosts from it and opens the relay stream port toward the stream host
// just the same, so a relay session is opened with the target port the request
// asked for; nothing else about it is different.
//
// The response is newline-terminated on the wire; the terminator is not part of
// any field.
type AppResponse struct {
	// Fields is the number of whitespace fields the response had: 8 or 4.
	Fields           int
	ConnectionNumber int    // "num"; reused as RelayOpen.ConnectionNumber
	StreamHost       string // relay stream host; RelayStreamPort is opened toward it
	ControlHost      string // relay control hostname

	// The eight-field form alone carries the fields below; the four-field form
	// leaves them zero.
	TargetPort int    // echoed camera target port
	DirectIP   string // camera LAN endpoint for the tryDirect attempt
	DirectPort int    // camera LAN endpoint port
	Mode       int    // final field; runtime-confirmed as the connection mode
}

// RelayRefusedError is the control host declining to open a session. The
// native parser accepts a response only when num > 0, so a non-positive num is
// how the relay says no. Observed against the real relay (2026-09-17) it came
// in runs lasting minutes to hours, each ending with the camera's connection
// numbers starting over at 1 — the camera had dropped off the relay and
// registered again. It means "no camera to connect you to", not "try again in
// a second": in 528 measured retries within the same session, not one
// succeeded.
type RelayRefusedError struct {
	ConnectionNumber int
	// Response is the response line as received, without its terminator and
	// cut to a readable length, so a reason the relay sends along reaches the
	// log. It is formatted with %q wherever it is printed.
	Response string
}

func (e *RelayRefusedError) Error() string {
	return fmt.Sprintf("relay refused the session (response %q): the camera is not connected to the relay", e.Response)
}

// maxDescribedResponse bounds how much of an unexpected response line an error
// message repeats. The reconstructed forms are well under this.
const maxDescribedResponse = 160

func describeResponse(text string) string {
	if len(text) > maxDescribedResponse {
		return text[:maxDescribedResponse] + "..."
	}
	return text
}

func ParseAppResponse(data []byte) (AppResponse, error) {
	var result AppResponse
	text := strings.TrimRight(string(data), "\r\n")
	fields := strings.Split(text, " ")
	if len(fields) < 2 || !strings.EqualFold(fields[0], "app") {
		return result, fmt.Errorf("response must begin with the app keyword (response %q)", describeResponse(text))
	}

	num, err := strconv.Atoi(fields[1])
	if err != nil {
		return result, fmt.Errorf("connection number (response %q): %w", describeResponse(text), err)
	}
	// The native parser requires num > 0 before accepting the response. A
	// refusal is reported as its own type: it is the relay's answer, not a
	// transport fault, and callers treat the two differently.
	if num <= 0 {
		return result, &RelayRefusedError{ConnectionNumber: num, Response: describeResponse(text)}
	}
	result.ConnectionNumber = num

	// The two-field and three-field forms are acknowledged by the native
	// parser but have not come from the relay, so they are rejected here
	// rather than guessed. The line itself is kept in the error: the first
	// four-field response seen in the field could not be interpreted
	// afterwards because it was not.
	switch len(fields) {
	case 4, 8:
	default:
		return result, fmt.Errorf("unsupported response field count %d (response %q); only the four-field and eight-field forms are reconstructed", len(fields), describeResponse(text))
	}
	result.Fields = len(fields)
	result.StreamHost = fields[2]
	result.ControlHost = fields[3]
	if err := validateHost("stream host", result.StreamHost); err != nil {
		return result, err
	}
	if err := validateHost("control host", result.ControlHost); err != nil {
		return result, err
	}
	if len(fields) == 4 {
		return result, nil
	}

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
