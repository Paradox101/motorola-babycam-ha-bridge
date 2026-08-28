// Package rtspmini is a minimal but real RTSP-over-TCP camera and client used to
// demonstrate and test that media flows end to end through the Magic tunnel and
// the bridge. It implements just enough of RTSP/1.0 (OPTIONS, DESCRIBE, SETUP,
// PLAY) plus interleaved RTP framing to carry recognizable payloads.
package rtspmini

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const rtpChannel = 0

// FramePayload is the deterministic RTP packet for frame i: a 12-byte RTP
// header (version 2, dynamic payload type 96, incrementing sequence and
// timestamp) followed by a recognizable marker standing in for an H.264 NAL.
func FramePayload(i int) []byte {
	pkt := make([]byte, 12)
	pkt[0] = 0x80 // version 2
	pkt[1] = 96   // dynamic payload type
	binary.BigEndian.PutUint16(pkt[2:4], uint16(i+1))
	binary.BigEndian.PutUint32(pkt[4:8], uint32((i+1)*3000))
	binary.BigEndian.PutUint32(pkt[8:12], 0xCAFEBABE)
	return append(pkt, []byte(fmt.Sprintf("FRAME-%03d-h264-nal", i))...)
}

// ServeCamera serves one RTSP client on conn: it answers the handshake and,
// after PLAY, streams frames interleaved RTP packets, then returns.
func ServeCamera(conn net.Conn, frames int) error {
	defer conn.Close()
	br := bufio.NewReader(conn)

	sdp := strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 127.0.0.1",
		"s=magic-camera",
		"t=0 0",
		"m=video 0 RTP/AVP 96",
		"a=rtpmap:96 H264/90000",
		"a=control:streamid=0",
		"",
	}, "\r\n")

	for {
		method, cseq, err := readRequest(br)
		if err != nil {
			return err
		}
		switch method {
		case "OPTIONS":
			writeString(conn, "RTSP/1.0 200 OK\r\nCSeq: "+cseq+"\r\nPublic: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN\r\n\r\n")
		case "DESCRIBE":
			writeString(conn, fmt.Sprintf("RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, len(sdp), sdp))
		case "SETUP":
			writeString(conn, "RTSP/1.0 200 OK\r\nCSeq: "+cseq+"\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\nSession: 12345678\r\n\r\n")
		case "PLAY":
			writeString(conn, "RTSP/1.0 200 OK\r\nCSeq: "+cseq+"\r\nSession: 12345678\r\nRTP-Info: url=streamid=0;seq=1;rtptime=0\r\n\r\n")
			return streamRTP(conn, frames)
		default:
			writeString(conn, "RTSP/1.0 501 Not Implemented\r\nCSeq: "+cseq+"\r\n\r\n")
		}
	}
}

func streamRTP(conn net.Conn, frames int) error {
	for i := 0; i < frames; i++ {
		payload := FramePayload(i)
		frame := make([]byte, 0, 4+len(payload))
		frame = append(frame, '$', byte(rtpChannel))
		var length [2]byte
		binary.BigEndian.PutUint16(length[:], uint16(len(payload)))
		frame = append(frame, length[:]...)
		frame = append(frame, payload...)
		if _, err := conn.Write(frame); err != nil {
			return err
		}
		time.Sleep(2 * time.Millisecond)
	}
	return nil
}

// PullFrames performs a full RTSP handshake against addr and returns the
// payloads of the first `frames` interleaved RTP packets.
func PullFrames(addr string, frames int, timeout time.Duration) ([][]byte, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))
	br := bufio.NewReader(conn)

	base := "rtsp://127.0.0.1/owner/streaming"
	if err := sendRequest(conn, "OPTIONS", base, 1, ""); err != nil {
		return nil, err
	}
	if _, err := readResponse(br); err != nil {
		return nil, err
	}
	if err := sendRequest(conn, "DESCRIBE", base, 2, "Accept: application/sdp\r\n"); err != nil {
		return nil, err
	}
	sdp, err := readResponse(br)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(sdp, "H264/90000") {
		return nil, fmt.Errorf("DESCRIBE lacked expected SDP: %q", sdp)
	}
	if err := sendRequest(conn, "SETUP", base+"/streamid=0", 3, "Transport: RTP/AVP/TCP;unicast;interleaved=0-1\r\n"); err != nil {
		return nil, err
	}
	if _, err := readResponse(br); err != nil {
		return nil, err
	}
	if err := sendRequest(conn, "PLAY", base, 4, "Session: 12345678\r\n"); err != nil {
		return nil, err
	}
	if _, err := readResponse(br); err != nil {
		return nil, err
	}

	out := make([][]byte, 0, frames)
	for len(out) < frames {
		payload, err := readInterleaved(br)
		if err != nil {
			return out, fmt.Errorf("read frame %d: %w", len(out), err)
		}
		out = append(out, payload)
	}
	return out, nil
}

func sendRequest(conn net.Conn, method, uri string, cseq int, extra string) error {
	req := fmt.Sprintf("%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: rtspmini\r\n%s\r\n", method, uri, cseq, extra)
	_, err := conn.Write([]byte(req))
	return err
}

func readRequest(br *bufio.Reader) (method, cseq string, err error) {
	first := true
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if first {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				return "", "", fmt.Errorf("empty request line")
			}
			method = fields[0]
			first = false
			continue
		}
		if line == "" {
			return method, cseq, nil
		}
		if key, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(key), "CSeq") {
			cseq = strings.TrimSpace(value)
		}
	}
}

func readResponse(br *bufio.Reader) (string, error) {
	status, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(status, "RTSP/1.0 200") {
		return "", fmt.Errorf("unexpected status: %q", strings.TrimSpace(status))
	}
	contentLength := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if key, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			contentLength, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if contentLength == 0 {
		return "", nil
	}
	body := make([]byte, contentLength)
	if _, err := readFull(br, body); err != nil {
		return "", err
	}
	return string(body), nil
}

func readInterleaved(br *bufio.Reader) ([]byte, error) {
	marker, err := br.ReadByte()
	if err != nil {
		return nil, err
	}
	if marker != '$' {
		return nil, fmt.Errorf("expected interleaved marker '$', got 0x%02x", marker)
	}
	if _, err := br.ReadByte(); err != nil { // channel
		return nil, err
	}
	var length [2]byte
	if _, err := readFull(br, length[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, int(binary.BigEndian.Uint16(length[:])))
	if _, err := readFull(br, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeString(conn net.Conn, s string) {
	_, _ = conn.Write([]byte(s))
}

func readFull(br *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := br.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
