package mqttdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Host               string
	Port               int
	Username           string
	Password           string
	DiscoveryPrefix    string
	ClientID           string
	OnConnectionChange func(bool)
}

type Camera struct {
	ID        string
	Name      string
	Model     string
	StreamURL string
}

type clientConfig struct {
	Config
	WillTopic   string
	WillPayload string
}

type brokerClient interface {
	Start(context.Context, func(), func(error)) error
	Publish(context.Context, string, bool, []byte) error
	Close(uint)
}

type clientFactory func(clientConfig) brokerClient

type Service struct {
	mu           sync.RWMutex
	config       Config
	client       brokerClient
	connected    bool
	cameras      map[string]Camera
	availability string
}

func NewService(config Config) *Service {
	return newService(config, func(config clientConfig) brokerClient {
		return newPahoClient(config)
	})
}

func newService(config Config, factory clientFactory) *Service {
	prefix := strings.Trim(config.DiscoveryPrefix, "/")
	config.DiscoveryPrefix = prefix
	return &Service{
		config:       config,
		client:       factory(clientConfig{Config: config, WillTopic: prefix + "/device/motorola_nursery_bridge/availability", WillPayload: "offline"}),
		cameras:      make(map[string]Camera),
		availability: prefix + "/device/motorola_nursery_bridge/availability",
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s.config.Host == "" || s.config.Port < 1 || s.config.Port > 65535 || s.config.DiscoveryPrefix == "" {
		return errors.New("mqtt discovery requires host, valid port and discovery prefix")
	}
	return s.client.Start(ctx, s.onConnect, func(error) {
		s.mu.Lock()
		s.connected = false
		s.mu.Unlock()
		if s.config.OnConnectionChange != nil {
			s.config.OnConnectionChange(false)
		}
	})
}

func (s *Service) Upsert(ctx context.Context, camera Camera) error {
	if camera.ID == "" || camera.Name == "" || camera.StreamURL == "" {
		return errors.New("mqtt camera requires id, name and stream URL")
	}
	parsed, err := url.Parse(camera.StreamURL)
	if err != nil || parsed.Scheme != "rtsp" || parsed.Host == "" {
		return errors.New("mqtt camera stream URL must be absolute RTSP")
	}
	s.mu.Lock()
	s.cameras[camera.ID] = camera
	connected := s.connected
	s.mu.Unlock()
	if !connected {
		return nil
	}
	return s.publishCamera(ctx, camera)
}

func (s *Service) Remove(ctx context.Context, id string) error {
	s.mu.Lock()
	delete(s.cameras, id)
	connected := s.connected
	s.mu.Unlock()
	if !connected {
		return nil
	}
	topic := s.config.DiscoveryPrefix + "/camera/" + objectID(id) + "/config"
	return s.client.Publish(ctx, topic, true, nil)
}

func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	connected := s.connected
	s.connected = false
	s.mu.Unlock()
	var err error
	if connected {
		err = s.client.Publish(ctx, s.availability, true, []byte("offline"))
	}
	s.client.Close(1000)
	return err
}

func (s *Service) onConnect() {
	s.mu.Lock()
	s.connected = true
	cameras := make([]Camera, 0, len(s.cameras))
	for _, camera := range s.cameras {
		cameras = append(cameras, camera)
	}
	s.mu.Unlock()
	if s.config.OnConnectionChange != nil {
		s.config.OnConnectionChange(true)
	}
	sort.Slice(cameras, func(i, j int) bool { return cameras[i].ID < cameras[j].ID })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.client.Publish(ctx, s.availability, true, []byte("online"))
	for _, camera := range cameras {
		_ = s.publishCamera(ctx, camera)
	}
}

func (s *Service) publishCamera(ctx context.Context, camera Camera) error {
	model := camera.Model
	if model == "" {
		model = "Nursery Camera"
	}
	payload := struct {
		Name                string `json:"name"`
		UniqueID            string `json:"unique_id"`
		AvailabilityTopic   string `json:"availability_topic"`
		PayloadAvailable    string `json:"payload_available"`
		PayloadNotAvailable string `json:"payload_not_available"`
		StreamSource        string `json:"stream_source"`
		Device              struct {
			Identifiers  []string `json:"identifiers"`
			Name         string   `json:"name"`
			Manufacturer string   `json:"manufacturer"`
			Model        string   `json:"model"`
		} `json:"device"`
	}{
		Name:                camera.Name,
		UniqueID:            camera.ID,
		AvailabilityTopic:   s.availability,
		PayloadAvailable:    "online",
		PayloadNotAvailable: "offline",
		StreamSource:        camera.StreamURL,
	}
	payload.Device.Identifiers = []string{camera.ID}
	payload.Device.Name = camera.Name
	payload.Device.Manufacturer = "Motorola"
	payload.Device.Model = model
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	topic := s.config.DiscoveryPrefix + "/camera/" + objectID(camera.ID) + "/config"
	return s.client.Publish(ctx, topic, true, data)
}

func objectID(id string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(id) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "camera"
	}
	return result
}

type pahoClient struct {
	config clientConfig
	client mqtt.Client
}

func newPahoClient(config clientConfig) *pahoClient { return &pahoClient{config: config} }

func (p *pahoClient) Start(ctx context.Context, onConnect func(), onLost func(error)) error {
	options := mqtt.NewClientOptions()
	options.AddBroker("tcp://" + p.config.Host + ":" + strconv.Itoa(p.config.Port))
	clientID := p.config.ClientID
	if clientID == "" {
		clientID = "motorola-nursery-bridge"
	}
	options.SetClientID(clientID)
	options.SetUsername(p.config.Username)
	options.SetPassword(p.config.Password)
	options.SetAutoReconnect(true)
	options.SetConnectRetry(true)
	options.SetConnectRetryInterval(5 * time.Second)
	options.SetMaxReconnectInterval(time.Minute)
	options.SetKeepAlive(30 * time.Second)
	options.SetPingTimeout(10 * time.Second)
	options.SetOrderMatters(false)
	options.SetWill(p.config.WillTopic, p.config.WillPayload, 1, true)
	options.SetOnConnectHandler(func(mqtt.Client) { onConnect() })
	options.SetConnectionLostHandler(func(_ mqtt.Client, err error) { onLost(err) })
	p.client = mqtt.NewClient(options)
	return waitToken(ctx, p.client.Connect())
}

func (p *pahoClient) Publish(ctx context.Context, topic string, retained bool, payload []byte) error {
	if p.client == nil {
		return errors.New("mqtt client is not started")
	}
	return waitToken(ctx, p.client.Publish(topic, 1, retained, payload))
}

func (p *pahoClient) Close(quiesce uint) {
	if p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(quiesce)
	}
}

func waitToken(ctx context.Context, token mqtt.Token) error {
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			return ctx.Err()
		}
	}
	if !token.WaitTimeout(timeout) {
		return fmt.Errorf("mqtt operation timed out: %w", context.DeadlineExceeded)
	}
	return token.Error()
}
