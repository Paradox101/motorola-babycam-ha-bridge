package devicecontrol

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

type Camera struct {
	ID       string
	DeviceID uint32
	Token    string
	Host     string
	Port     int
}

type DialContextFunc func(context.Context, string, *tls.Config) (net.Conn, error)

type Client struct {
	RootCAs     *x509.CertPool
	DialContext DialContextFunc
	Timeout     time.Duration
}

type Connection struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (c Client) Connect(ctx context.Context, camera Camera) (*Connection, error) {
	if camera.Host == "" || camera.Port < 1 || camera.Port > 65535 {
		return nil, errors.New("device control requires a valid host and port")
	}
	address := net.JoinHostPort(camera.Host, strconv.Itoa(camera.Port))
	var conn net.Conn
	var err error
	if c.DialContext != nil {
		conn, err = c.DialContext(ctx, address, tlsConfig(camera.Host, c.RootCAs))
	} else {
		dialer := tls.Dialer{Config: tlsConfig(camera.Host, c.RootCAs)}
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect device control: %w", err)
	}
	connection := &Connection{conn: conn, reader: bufio.NewReader(conn)}
	command, err := AuthenticationCommand(camera.DeviceID, camera.Token)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := connection.request(ctx, command, ParseAuthentication); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return connection, nil
}

func (c *Connection) SupportsTemperature(ctx context.Context) (bool, error) {
	var supported bool
	err := c.request(ctx, "caplist\n", func(line string) error {
		value, err := ParseTemperatureCapability(line)
		supported = value
		return err
	})
	return supported, err
}

func (c *Connection) Temperature(ctx context.Context) (float64, error) {
	var temperature float64
	err := c.request(ctx, "get 1 temperature_reading\n", func(line string) error {
		value, err := ParseTemperature(line)
		temperature = value
		return err
	})
	return temperature, err
}

func (c *Connection) Close() error { return c.conn.Close() }

func (c *Connection) request(ctx context.Context, command string, parse func(string) error) error {
	deadline := time.Now().Add(10 * time.Second)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set device control deadline: %w", err)
	}
	defer c.conn.SetDeadline(time.Time{})
	if _, err := c.conn.Write([]byte(command)); err != nil {
		return fmt.Errorf("write device control request: %w", err)
	}
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read device control response: %w", err)
	}
	if err := parse(line); err != nil {
		return err
	}
	return nil
}

func tlsConfig(host string, roots *x509.CertPool) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: roots}
}
