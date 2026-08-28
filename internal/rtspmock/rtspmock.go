// Package rtspmock provides a minimal RTSP camera server and client that speak
// a realistic session — OPTIONS, DESCRIBE, SETUP, PLAY with interleaved
// (RTP-over-TCP) media, then TEARDOWN. It exists to validate the bridge's
// byte-transparency for a full session, including binary interleaved frames,
// entirely offline with no Android and no live credentials.
//
// It is not a complete RTSP implementation; it covers exactly the shape the
// VM65 tunnel carries: a single TCP connection with interleaved media.
package rtspmock

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// DefaultSDP mirrors the media the VM65 delivers through the tunnel: H.264 video
// and PCMA/8000 audio, as documented in docs/current-state.md. It carries no
// device-specific or secret values.
const DefaultSDP = "v=0\r\n" +
	"o=- 0 0 IN IP4 127.0.0.1\r\n" +
	"s=owner/streaming\r\n" +
	"t=0 0\r\n" +
	"m=video 0 RTP/AVP 96\r\n" +
	"a=rtpmap:96 H264/90000\r\n" +
	"a=control:camera\r\n" +
	"m=audio 0 RTP/AVP 8\r\n" +
	"a=rtpmap:8 PCMA/8000\r\n" +
	"a=control:micphone\r\n"

// Camera is a mock RTSP camera. Zero value is usable; SDP defaults to
// DefaultSDP and RTPPackets to a small synthetic burst.
type Camera struct {
	// SDP is returned for DESCRIBE. Empty selects DefaultSDP.
	SDP string
	// RTPPackets are emitted, in order, as interleaved frames after PLAY. Empty
	// selects a synthetic burst from SyntheticRTP. Each is sent verbatim so the
	// test can assert byte-exact delivery, including high-entropy payloads.
	RTPPackets [][]byte
	// Channel is the interleaved channel byte used for the RTP frames.
	Channel byte
}

// Serve accepts connections on l and handles each as one RTSP session until l
// is closed. It is meant to run in a goroutine.
func (c *Camera) Serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go c.handle(conn)
	}
}

func (c *Camera) sdp() string {
	if c.SDP != "" {
		return c.SDP
	}
	return DefaultSDP
}

func (c *Camera) packets() [][]byte {
	if len(c.RTPPackets) > 0 {
		return c.RTPPackets
	}
	return SyntheticRTP(8)
}

func (c *Camera) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	session := "12345678"
	for {
		req, err := readMessage(reader)
		if err != nil {
			return
		}
		switch req.Method {
		case "OPTIONS":
			writeResponse(conn, req.CSeq, map[string]string{
				"Public": "OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN",
			}, nil)
		case "DESCRIBE":
			writeResponse(conn, req.CSeq, map[string]string{
				"Content-Type": "application/sdp",
			}, []byte(c.sdp()))
		case "SETUP":
			writeResponse(conn, req.CSeq, map[string]string{
				"Session":   session,
				"Transport": req.Headers["transport"],
			}, nil)
		case "PLAY":
			writeResponse(conn, req.CSeq, map[string]string{
				"Session":  session,
				"RTP-Info": "url=camera",
			}, nil)
			for _, pkt := range c.packets() {
				if err := writeInterleaved(conn, c.Channel, pkt); err != nil {
					return
				}
			}
		case "TEARDOWN":
			writeResponse(conn, req.CSeq, map[string]string{"Session": session}, nil)
			return
		default:
			writeStatus(conn, req.CSeq, "501 Not Implemented")
		}
	}
}

// request is a parsed RTSP request. Header keys are lower-cased.
type request struct {
	Method  string
	Target  string
	CSeq    string
	Headers map[string]string
}

func readMessage(r *bufio.Reader) (request, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return request{}, err
	}
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return request{}, errors.New("malformed request line")
	}
	req := request{Method: parts[0], Target: parts[1], Headers: map[string]string{}}
	for {
		h, err := r.ReadString('\n')
		if err != nil {
			return request{}, err
		}
		h = strings.TrimRight(h, "\r\n")
		if h == "" {
			break
		}
		key, value, ok := strings.Cut(h, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		req.Headers[key] = value
		if key == "cseq" {
			req.CSeq = value
		}
	}
	// None of the handled methods carry a body; drain Content-Length if present.
	if cl := req.Headers["content-length"]; cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n > 0 {
			if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
				return request{}, err
			}
		}
	}
	return req, nil
}

func writeResponse(w io.Writer, cseq string, headers map[string]string, body []byte) {
	var b strings.Builder
	b.WriteString("RTSP/1.0 200 OK\r\n")
	if cseq != "" {
		fmt.Fprintf(&b, "CSeq: %s\r\n", cseq)
	}
	for k, v := range headers {
		if v == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	if body != nil {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	}
	b.WriteString("\r\n")
	_, _ = io.WriteString(w, b.String())
	if body != nil {
		_, _ = w.Write(body)
	}
}

func writeStatus(w io.Writer, cseq, status string) {
	fmt.Fprintf(w, "RTSP/1.0 %s\r\nCSeq: %s\r\n\r\n", status, cseq)
}

// writeInterleaved emits one RTP-over-TCP frame: '$', channel, 16-bit length,
// payload.
func writeInterleaved(w io.Writer, channel byte, payload []byte) error {
	if len(payload) > 0xffff {
		return errors.New("interleaved payload too large")
	}
	header := []byte{'$', channel, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// SyntheticRTP returns n deterministic pseudo-RTP packets whose payloads are
// high-entropy, so a transparency test proves the tunnel carries arbitrary
// binary bytes, not just text. Packet i has a valid-looking 12-byte RTP header
// followed by a varying-length body.
func SyntheticRTP(n int) [][]byte {
	packets := make([][]byte, n)
	for i := 0; i < n; i++ {
		size := 12 + 20 + i*7
		p := make([]byte, size)
		p[0] = 0x80                                  // version 2
		p[1] = 96                                    // payload type
		binary.BigEndian.PutUint16(p[2:], uint16(i)) // sequence
		binary.BigEndian.PutUint32(p[4:], uint32(i*3000))
		binary.BigEndian.PutUint32(p[8:], 0xDEADBEEF)
		// Deterministic high-entropy body.
		seed := uint32(i*2654435761 + 1)
		for j := 12; j < size; j++ {
			seed = seed*1664525 + 1013904223
			p[j] = byte(seed >> 24)
		}
		packets[i] = p
	}
	return packets
}
