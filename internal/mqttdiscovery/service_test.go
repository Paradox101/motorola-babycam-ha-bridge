package mqttdiscovery

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type publishedMessage struct {
	topic    string
	payload  []byte
	retained bool
}

type fakeBrokerClient struct {
	mu        sync.Mutex
	onConnect func()
	messages  []publishedMessage
}

func (f *fakeBrokerClient) Start(_ context.Context, onConnect func(), _ func(error)) error {
	f.onConnect = onConnect
	onConnect()
	return nil
}

func (f *fakeBrokerClient) Publish(_ context.Context, topic string, retained bool, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, publishedMessage{topic, append([]byte(nil), payload...), retained})
	return nil
}

func (f *fakeBrokerClient) Close(uint) {}

func (f *fakeBrokerClient) reconnect() { f.onConnect() }

func (f *fakeBrokerClient) countTopic(topic string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, message := range f.messages {
		if message.topic == topic {
			count++
		}
	}
	return count
}

// last returns the most recent payload published to topic.
func (f *fakeBrokerClient) last(topic string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.messages) - 1; index >= 0; index-- {
		if f.messages[index].topic == topic {
			return f.messages[index].payload, true
		}
	}
	return nil, false
}

func (f *fakeBrokerClient) config(t *testing.T, topic string) map[string]any {
	t.Helper()
	raw, ok := f.last(topic)
	if !ok {
		t.Fatalf("nothing was published to %s", topic)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s payload is not JSON: %v", topic, err)
	}
	return payload
}

func newTestService(t *testing.T) (*Service, *fakeBrokerClient) {
	t.Helper()
	return newConfiguredService(t, nil)
}

// newConfiguredService builds a service with one setting adjusted, so a test
// about camera frames does not need its own copy of the base configuration.
func newConfiguredService(t *testing.T, adjust func(*Config)) (*Service, *fakeBrokerClient) {
	t.Helper()
	fake := &fakeBrokerClient{}
	config := Config{
		Host:             "broker",
		Port:             1883,
		DiscoveryPrefix:  "homeassistant",
		Version:          "v9.9.9",
		ConfigurationURL: "http://homeassistant.local:1984",
	}
	if adjust != nil {
		adjust(&config)
	}
	service := newService(config, func(clientConfig) brokerClient { return fake })
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service, fake
}

func bundledCamera() Camera {
	return Camera{
		ID:          "camera-a",
		Name:        "Baby Room",
		Model:       "VM65CONNECT",
		StreamURL:   "rtsp://host:8555/vm65",
		SnapshotURL: "http://homeassistant.local:1984/api/frame.jpeg?src=vm65",
	}
}

// The MQTT camera platform requires an image `topic` and has no stream_source
// key, so a camera discovery payload is rejected outright by Home Assistant and
// creates no entity. Cameras must therefore be published as image entities.
func TestCamerasArePublishedAsImageEntitiesNotAsMQTTCameras(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}

	payload := fake.config(t, "homeassistant/image/camera-a/config")
	if payload["url_topic"] != "motorola-nursery-bridge/camera/camera-a/snapshot_url" {
		t.Fatalf("url_topic = %#v", payload["url_topic"])
	}
	for _, rejected := range []string{"stream_source", "still_image_url", "topic"} {
		if _, present := payload[rejected]; present {
			t.Fatalf("image payload carries %q, which the image platform does not accept", rejected)
		}
	}

	url, ok := fake.last("motorola-nursery-bridge/camera/camera-a/snapshot_url")
	if !ok || string(url) != "http://homeassistant.local:1984/api/frame.jpeg?src=vm65" {
		t.Fatalf("snapshot url = %q", url)
	}
}

// Anyone upgrading has a retained, permanently invalid camera config sitting on
// their broker. It has to be cleared or Home Assistant keeps logging it.
func TestUpsertClearsTheLegacyCameraDiscoveryTopic(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	payload, ok := fake.last("homeassistant/camera/camera-a/config")
	if !ok {
		t.Fatal("the legacy camera discovery topic was never cleared")
	}
	if len(payload) != 0 {
		t.Fatalf("legacy camera config was not cleared, payload = %q", payload)
	}
}

func TestBridgeDiagnosticsDescribeTheRunningProcess(t *testing.T) {
	service, fake := newTestService(t)

	connection := fake.config(t, "homeassistant/binary_sensor/motorola_nursery_bridge_connection/config")
	if connection["device_class"] != "connectivity" {
		t.Fatalf("device_class = %#v", connection["device_class"])
	}
	// The connectivity sensor must not carry availability: an unavailable
	// entity cannot report that the bridge is disconnected.
	if _, present := connection["availability"]; present {
		t.Fatal("the connection sensor carries availability and can never report 'off'")
	}
	device := connection["device"].(map[string]any)
	if device["sw_version"] != "v9.9.9" || device["configuration_url"] != "http://homeassistant.local:1984" {
		t.Fatalf("bridge device = %#v", device)
	}

	sessions := fake.config(t, "homeassistant/sensor/motorola_nursery_bridge_active_sessions/config")
	if sessions["state_class"] != "measurement" || sessions["entity_category"] != "diagnostic" {
		t.Fatalf("active sessions sensor = %#v", sessions)
	}
	restarts := fake.config(t, "homeassistant/sensor/motorola_nursery_bridge_reconnects/config")
	if restarts["state_class"] != "total_increasing" {
		t.Fatalf("restart sensor state_class = %#v", restarts["state_class"])
	}

	if err := service.PublishStatus(context.Background(), Status{ActiveSessions: 3, Reconnects: 7}); err != nil {
		t.Fatal(err)
	}
	if value, _ := fake.last("motorola-nursery-bridge/status/active_sessions"); string(value) != "3" {
		t.Fatalf("active sessions = %q", value)
	}
	if value, _ := fake.last("motorola-nursery-bridge/status/reconnects"); string(value) != "7" {
		t.Fatalf("reconnects = %q", value)
	}
}

func TestCameraEntitiesHangOffTheBridgeDeviceAndTrackTheirOwnAvailability(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}

	image := fake.config(t, "homeassistant/image/camera-a/config")
	device := image["device"].(map[string]any)
	if device["via_device"] != bridgeObjectID {
		t.Fatalf("camera device is not linked to the bridge: %#v", device)
	}
	if device["model"] != "VM65CONNECT" {
		t.Fatalf("model = %#v", device["model"])
	}
	if image["unique_id"] != "camera-a_snapshot" {
		t.Fatalf("unique_id = %#v", image["unique_id"])
	}
	if image["availability_mode"] != "all" {
		t.Fatalf("availability_mode = %#v", image["availability_mode"])
	}
	if entries := image["availability"].([]any); len(entries) != 2 {
		t.Fatalf("expected bridge and camera availability, got %#v", entries)
	}

	// A single failed camera must show as unavailable while the others stay up.
	if err := service.SetCameraAvailable(context.Background(), "camera-a", false); err != nil {
		t.Fatal(err)
	}
	if value, _ := fake.last("motorola-nursery-bridge/camera/camera-a/availability"); string(value) != "offline" {
		t.Fatalf("camera availability = %q", value)
	}
}

func TestExternalModeCameraStillGetsALinkSensorButNoImage(t *testing.T) {
	service, fake := newTestService(t)
	camera := bundledCamera()
	camera.SnapshotURL = ""
	if err := service.Upsert(context.Background(), camera); err != nil {
		t.Fatal(err)
	}

	link := fake.config(t, "homeassistant/binary_sensor/camera-a_link/config")
	if link["device_class"] != "connectivity" {
		t.Fatalf("link sensor = %#v", link)
	}
	payload, ok := fake.last("homeassistant/image/camera-a/config")
	if !ok {
		t.Fatal("the image discovery topic was never addressed in external mode")
	}
	if len(payload) != 0 {
		t.Fatalf("an image entity was published without a snapshot URL: %q", payload)
	}
}

func TestTemperatureSensorPublishesCelsiusAndDedicatedAvailability(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	if err := service.SetTemperatureSupported(context.Background(), "camera-a", true); err != nil {
		t.Fatal(err)
	}
	sensor := fake.config(t, "homeassistant/sensor/camera-a_temperature/config")
	if sensor["device_class"] != "temperature" || sensor["state_class"] != "measurement" || sensor["unit_of_measurement"] != "°C" {
		t.Fatalf("temperature sensor = %#v", sensor)
	}
	if sensor["state_topic"] != "motorola-nursery-bridge/camera/camera-a/temperature" || sensor["availability_mode"] != "all" {
		t.Fatalf("temperature topics = %#v", sensor)
	}
	if entries := sensor["availability"].([]any); len(entries) != 2 {
		t.Fatalf("temperature availability = %#v", entries)
	}
	if err := service.PublishTemperature(context.Background(), "camera-a", 21.4); err != nil {
		t.Fatal(err)
	}
	if value, _ := fake.last("motorola-nursery-bridge/camera/camera-a/temperature"); string(value) != "21.4" {
		t.Fatalf("temperature state = %q", value)
	}
	if value, _ := fake.last("motorola-nursery-bridge/camera/camera-a/temperature_availability"); string(value) != "online" {
		t.Fatalf("temperature availability = %q", value)
	}
}

func TestUnsupportedTemperatureRetiresSensor(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	if err := service.SetTemperatureSupported(context.Background(), "camera-a", true); err != nil {
		t.Fatal(err)
	}
	if err := service.SetTemperatureSupported(context.Background(), "camera-a", false); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{
		"homeassistant/sensor/camera-a_temperature/config",
		"motorola-nursery-bridge/camera/camera-a/temperature",
		"motorola-nursery-bridge/camera/camera-a/temperature_availability",
	} {
		payload, ok := fake.last(topic)
		if !ok || len(payload) != 0 {
			t.Fatalf("%s was not retired, payload=%q", topic, payload)
		}
	}
}

func TestServiceRepublishesEverythingAfterReconnect(t *testing.T) {
	service, fake := newTestService(t)
	cameras := []Camera{
		bundledCamera(),
		{ID: "camera-b", Name: "Play Room", Model: "MBP99", StreamURL: "rtsp://host:8555/play-room"},
	}
	for _, camera := range cameras {
		if err := service.Upsert(context.Background(), camera); err != nil {
			t.Fatal(err)
		}
	}
	topics := []string{
		"homeassistant/image/camera-a/config",
		"homeassistant/binary_sensor/camera-a_link/config",
		"homeassistant/binary_sensor/camera-b_link/config",
		"homeassistant/binary_sensor/motorola_nursery_bridge_connection/config",
	}
	before := make(map[string]int, len(topics))
	for _, topic := range topics {
		before[topic] = fake.countTopic(topic)
		if before[topic] == 0 {
			t.Fatalf("%s was never published", topic)
		}
	}

	fake.reconnect()
	for _, topic := range topics {
		if got := fake.countTopic(topic); got <= before[topic] {
			t.Fatalf("%s was not republished after reconnect (%d -> %d)", topic, before[topic], got)
		}
	}
	if value, _ := fake.last("motorola-nursery-bridge/availability"); string(value) != "online" {
		t.Fatalf("availability after reconnect = %q", value)
	}
}

func TestRemoveRetiresEveryEntityOfACamera(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(context.Background(), "camera-a"); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{
		"homeassistant/image/camera-a/config",
		"homeassistant/binary_sensor/camera-a_link/config",
	} {
		payload, ok := fake.last(topic)
		if !ok || len(payload) != 0 {
			t.Fatalf("%s was not retired, payload = %q", topic, payload)
		}
	}
}

func TestUpsertRejectsMalformedURLs(t *testing.T) {
	service, _ := newTestService(t)
	camera := bundledCamera()
	camera.StreamURL = "http://not-rtsp/stream"
	if err := service.Upsert(context.Background(), camera); err == nil {
		t.Fatal("a non-RTSP stream URL was accepted")
	}
	camera = bundledCamera()
	camera.SnapshotURL = "rtsp://not-http/frame"
	if err := service.Upsert(context.Background(), camera); err == nil {
		t.Fatal("a non-HTTP snapshot URL was accepted")
	}
}

// A camera entity is the only path by which Home Assistant discovers a camera
// at all, so the payload has to be one the camera platform accepts: image bytes
// on a topic, never a stream URL.
func TestCameraEntityIsPublishedWithATopicAndFedFrames(t *testing.T) {
	service, fake := newConfiguredService(t, func(config *Config) { config.PublishCameraFrames = true })
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}

	payload := fake.config(t, "homeassistant/camera/camera-a/config")
	if payload["topic"] != "motorola-nursery-bridge/camera/camera-a/image" {
		t.Fatalf("camera topic = %#v", payload["topic"])
	}
	if payload["unique_id"] != "camera-a_camera" {
		t.Fatalf("unique_id = %#v", payload["unique_id"])
	}
	for _, rejected := range []string{"stream_source", "url_topic", "state_topic"} {
		if _, present := payload[rejected]; present {
			t.Fatalf("camera payload carries %q, which the camera platform does not accept", rejected)
		}
	}

	jpeg := []byte{0xFF, 0xD8, 0x01, 0x02}
	if err := service.PublishFrame(context.Background(), "camera-a", jpeg); err != nil {
		t.Fatalf("PublishFrame: %v", err)
	}
	frame, ok := fake.last("motorola-nursery-bridge/camera/camera-a/image")
	if !ok || string(frame) != string(jpeg) {
		t.Fatalf("frame = %q", frame)
	}
}

// With frames off there is nothing to feed the entity, so it must not exist.
func TestFramesOffRetiresTheCameraEntity(t *testing.T) {
	service, fake := newTestService(t)
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	payload, ok := fake.last("homeassistant/camera/camera-a/config")
	if !ok || len(payload) != 0 {
		t.Fatalf("camera config = %q, want the retained payload cleared", payload)
	}
	if err := service.PublishFrame(context.Background(), "camera-a", []byte{0xFF, 0xD8}); err != nil {
		t.Fatalf("PublishFrame: %v", err)
	}
	if frame, published := fake.last("motorola-nursery-bridge/camera/camera-a/image"); published && len(frame) != 0 {
		t.Fatalf("image topic = %q, want nothing published with frames off", frame)
	}
}

func TestOnlyJPEGFramesFromRegisteredCamerasArePublished(t *testing.T) {
	service, _ := newConfiguredService(t, func(config *Config) { config.PublishCameraFrames = true })
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishFrame(context.Background(), "camera-a", []byte("<html>not an image")); err == nil {
		t.Fatal("expected a non-JPEG frame to be refused")
	}
	if err := service.PublishFrame(context.Background(), "unknown", []byte{0xFF, 0xD8}); err == nil {
		t.Fatal("expected an unregistered camera to be refused")
	}
}

// A camera that left the registry must not leave a retained frame behind.
func TestRemoveRetiresTheCameraEntityAndItsFrame(t *testing.T) {
	service, fake := newConfiguredService(t, func(config *Config) { config.PublishCameraFrames = true })
	if err := service.Upsert(context.Background(), bundledCamera()); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishFrame(context.Background(), "camera-a", []byte{0xFF, 0xD8}); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(context.Background(), "camera-a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, topic := range []string{
		"homeassistant/camera/camera-a/config",
		"motorola-nursery-bridge/camera/camera-a/image",
	} {
		payload, ok := fake.last(topic)
		if !ok || len(payload) != 0 {
			t.Fatalf("%s = %q, want it cleared", topic, payload)
		}
	}
}
