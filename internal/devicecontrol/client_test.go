package devicecontrol

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"testing"
)

func TestConnectionAuthenticatesDiscoversCapabilityAndReadsTemperature(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	serverDone := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(serverSide)
		steps := []struct{ want, reply string }{
			{"app 42 device-token\n", "app 1 0\n"},
			{"caplist\n", "caplist 1 temperature_reading r int 0 0\n"},
			{"get 1 temperature_reading\n", "get 1 temperature_reading 214\n"},
		}
		for _, step := range steps {
			line, err := reader.ReadString('\n')
			if err != nil {
				serverDone <- err
				return
			}
			if line != step.want {
				serverDone <- &unexpectedLine{got: line, want: step.want}
				return
			}
			if _, err := serverSide.Write([]byte(step.reply)); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	client := Client{DialContext: func(context.Context, string, *tls.Config) (net.Conn, error) { return clientSide, nil }}
	connection, err := client.Connect(context.Background(), Camera{ID: "camera", DeviceID: 42, Token: "device-token", Host: "camera.example", Port: 2288})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	supported, err := connection.SupportsTemperature(context.Background())
	if err != nil || !supported {
		t.Fatalf("supported=%t err=%v", supported, err)
	}
	temperature, err := connection.Temperature(context.Background())
	if err != nil || temperature != 21.4 {
		t.Fatalf("temperature=%v err=%v", temperature, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestTLSConfigUsesCameraHostnameAndVerification(t *testing.T) {
	config := tlsConfig("camera.example", nil)
	if config.ServerName != "camera.example" || config.InsecureSkipVerify {
		t.Fatalf("TLS config = %#v", config)
	}
}

type unexpectedLine struct{ got, want string }

func (e *unexpectedLine) Error() string { return "protocol line = " + e.got + ", want " + e.want }
