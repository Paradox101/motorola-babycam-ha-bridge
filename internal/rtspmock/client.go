package rtspmock

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Session is the result of driving a full RTSP dialogue: the negotiated SDP and
// the interleaved RTP payloads received after PLAY.
type Session struct {
	SDP string
	RTP [][]byte
}

// Dialogue runs OPTIONS, DESCRIBE, SETUP, PLAY over conn, reads wantPackets
// interleaved RTP frames, then TEARDOWN. conn is any byte-transparent stream —
// in the bridge tests it is the local bridge connection, so the whole exchange
// crosses the Magic WEB2 tunnel. The target is the RTSP URL path/authority the
// server echoes; it does not need to resolve.
func Dialogue(conn io.ReadWriter, target string, wantPackets int) (Session, error) {
	r := bufio.NewReader(conn)
	cseq := 0
	next := func() int { cseq++; return cseq }

	// OPTIONS
	if _, _, err := roundTrip(conn, r, "OPTIONS", target, next(), nil); err != nil {
		return Session{}, fmt.Errorf("OPTIONS: %w", err)
	}

	// DESCRIBE -> SDP body
	_, sdp, err := roundTrip(conn, r, "DESCRIBE", target, next(), map[string]string{"Accept": "application/sdp"})
	if err != nil {
		return Session{}, fmt.Errorf("DESCRIBE: %w", err)
	}

	// SETUP (interleaved transport)
	if _, _, err := roundTrip(conn, r, "SETUP", target+"/camera", next(),
		map[string]string{"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1"}); err != nil {
		return Session{}, fmt.Errorf("SETUP: %w", err)
	}

	// PLAY, then read the interleaved media.
	if err := writeRequest(conn, "PLAY", target, next(), map[string]string{"Range": "npt=0.000-"}); err != nil {
		return Session{}, fmt.Errorf("PLAY write: %w", err)
	}
	if _, _, err := readResponse(r); err != nil {
		return Session{}, fmt.Errorf("PLAY response: %w", err)
	}

	rtp := make([][]byte, 0, wantPackets)
	for len(rtp) < wantPackets {
		payload, err := readInterleaved(r)
		if err != nil {
			return Session{}, fmt.Errorf("read RTP %d/%d: %w", len(rtp), wantPackets, err)
		}
		rtp = append(rtp, payload)
	}

	// TEARDOWN (best-effort).
	_ = writeRequest(conn, "TEARDOWN", target, next(), nil)
	return Session{SDP: string(sdp), RTP: rtp}, nil
}

func roundTrip(w io.Writer, r *bufio.Reader, method, target string, cseq int, headers map[string]string) (map[string]string, []byte, error) {
	if err := writeRequest(w, method, target, cseq, headers); err != nil {
		return nil, nil, err
	}
	return readResponse(r)
}

func writeRequest(w io.Writer, method, target string, cseq int, headers map[string]string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s RTSP/1.0\r\n", method, target)
	fmt.Fprintf(&b, "CSeq: %d\r\n", cseq)
	for k, v := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func readResponse(r *bufio.Reader) (map[string]string, []byte, error) {
	status, err := r.ReadString('\n')
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(status, "RTSP/1.0 200") {
		return nil, nil, fmt.Errorf("non-200 status: %q", strings.TrimSpace(status))
	}
	headers := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	var body []byte
	if cl := headers["content-length"]; cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil {
			return nil, nil, fmt.Errorf("bad Content-Length %q", cl)
		}
		body = make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, nil, err
		}
	}
	return headers, body, nil
}

// readInterleaved reads one '$' framed RTP-over-TCP packet, skipping any RTSP
// response lines that the server may interleave between media frames.
func readInterleaved(r *bufio.Reader) ([]byte, error) {
	for {
		prefix, err := r.Peek(1)
		if err != nil {
			return nil, err
		}
		if prefix[0] != '$' {
			// An interleaved RTSP message; consume its line and continue.
			if _, err := r.ReadString('\n'); err != nil {
				return nil, err
			}
			continue
		}
		header := make([]byte, 4)
		if _, err := io.ReadFull(r, header); err != nil {
			return nil, err
		}
		if header[0] != '$' {
			return nil, errors.New("desynchronised interleaved frame")
		}
		length := binary.BigEndian.Uint16(header[2:])
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
}
