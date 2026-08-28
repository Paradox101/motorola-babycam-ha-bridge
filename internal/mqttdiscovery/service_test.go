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

func TestServicePublishesEveryCameraAndRepublishesAfterReconnect(t *testing.T) {
	fake := &fakeBrokerClient{}
	service := newService(Config{Host: "broker", Port: 1883, DiscoveryPrefix: "homeassistant"}, func(clientConfig) brokerClient {
		return fake
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cameras := []Camera{
		{ID: "camera-a", Name: "Baby Room", Model: "VM65CONNECT", StreamURL: "rtsp://host/a"},
		{ID: "camera-b", Name: "Play Room", Model: "MBP99", StreamURL: "rtsp://host/b"},
	}
	for _, camera := range cameras {
		if err := service.Upsert(context.Background(), camera); err != nil {
			t.Fatal(err)
		}
	}
	for _, camera := range cameras {
		topic := "homeassistant/camera/" + camera.ID + "/config"
		if fake.countTopic(topic) != 1 {
			t.Fatalf("%s initial count = %d", topic, fake.countTopic(topic))
		}
	}
	fake.reconnect()
	for _, camera := range cameras {
		topic := "homeassistant/camera/" + camera.ID + "/config"
		if fake.countTopic(topic) != 2 {
			t.Fatalf("%s reconnect count = %d", topic, fake.countTopic(topic))
		}
	}
}

func TestDiscoveryPayloadUsesReportedModelAndStableUniqueID(t *testing.T) {
	fake := &fakeBrokerClient{}
	service := newService(Config{Host: "broker", Port: 1883, DiscoveryPrefix: "homeassistant"}, func(clientConfig) brokerClient { return fake })
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.Upsert(context.Background(), Camera{ID: "stable-id", Name: "Room", Model: "MBP99", StreamURL: "rtsp://host/room"}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	for _, message := range fake.messages {
		if message.topic == "homeassistant/camera/stable-id/config" {
			if err := json.Unmarshal(message.payload, &payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	if payload["unique_id"] != "stable-id" {
		t.Fatalf("unique_id = %#v", payload["unique_id"])
	}
	device := payload["device"].(map[string]any)
	if device["model"] != "MBP99" {
		t.Fatalf("model = %#v", device["model"])
	}
}
