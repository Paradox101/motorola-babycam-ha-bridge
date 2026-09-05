package app

import (
	"net"
	"strconv"
	"testing"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
)

func TestBuildRegistryCreatesStableUniqueStreamNamesAndLegacyAlias(t *testing.T) {
	cameras := []bridge.Credentials{
		{DeviceID: 2, DeviceUDID: "z", DeviceName: "Baby Room"},
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "Baby Room"},
		{DeviceID: 3, DeviceUDID: "m", DeviceName: ""},
	}
	registry, err := BuildRegistry("127.0.0.1:8554", cameras)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Cameras) != 3 {
		t.Fatalf("camera count = %d", len(registry.Cameras))
	}
	wantNames := []string{"baby-room", "camera-m", "baby-room-2"}
	wantAddresses := []string{"127.0.0.1:8554", "127.0.0.1:9554", "127.0.0.1:9555"}
	for index, camera := range registry.Cameras {
		if camera.StreamName != wantNames[index] || camera.ListenAddr != wantAddresses[index] {
			t.Fatalf("camera %d = %#v", index, camera)
		}
	}
	if registry.LegacyAlias != "vm65" || registry.LegacyTarget != "baby-room" {
		t.Fatalf("legacy alias = %q -> %q", registry.LegacyAlias, registry.LegacyTarget)
	}
}

// A camera whose own name already reads like a deduplicated one must not take
// the name another camera was given: two cameras sharing a stream name share
// one go2rtc stream, one snapshot URL and one card on the page.
func TestBuildRegistryNamesStayUniqueAgainstADeduplicatedName(t *testing.T) {
	registry, err := BuildRegistry("127.0.0.1:8554", []bridge.Credentials{
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "Nursery"},
		{DeviceID: 2, DeviceUDID: "b", DeviceName: "Nursery"},
		{DeviceID: 3, DeviceUDID: "c", DeviceName: "Nursery 2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(registry.Cameras))
	for _, camera := range registry.Cameras {
		if seen[camera.StreamName] {
			names := make([]string, 0, len(registry.Cameras))
			for _, item := range registry.Cameras {
				names = append(names, item.StreamName)
			}
			t.Fatalf("duplicate stream name %q in %v", camera.StreamName, names)
		}
		seen[camera.StreamName] = true
	}
}

// A port something else already holds costs a camera nothing: the search moves
// up. Without this the bridge simply failed to bind, and retried forever.
func TestBuildSkipsAPortSomethingElseHolds(t *testing.T) {
	busy := map[int]bool{8554: true, 8555: true, 9555: true}
	registry, err := Build(BuildOptions{
		BaseAddress: "127.0.0.1:8554",
		Credentials: []bridge.Credentials{
			{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"},
			{DeviceID: 2, DeviceUDID: "b", DeviceName: "Room B"},
		},
		PortInUse: func(_ string, port int) bool { return busy[port] },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"127.0.0.1:8556", "127.0.0.1:9554"}
	for index, camera := range registry.Cameras {
		if camera.ListenAddr != want[index] {
			t.Fatalf("camera %d listens on %q, want %q", index, camera.ListenAddr, want[index])
		}
	}
}

// An address an earlier run recorded is the one the media server was configured
// with, so it is kept — including on a credential refresh, where it reads as
// busy precisely because this add-on's own bridge is serving on it.
func TestBuildKeepsARecordedAddressEvenWhenItReadsAsBusy(t *testing.T) {
	registry, err := Build(BuildOptions{
		BaseAddress: "127.0.0.1:8554",
		Credentials: []bridge.Credentials{
			{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"},
			{DeviceID: 2, DeviceUDID: "b", DeviceName: "Room B"},
		},
		Recorded:  map[string]string{"a": "127.0.0.1:8554", "b": "127.0.0.1:9554"},
		PortInUse: func(string, int) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Cameras[0].ListenAddr != "127.0.0.1:8554" || registry.Cameras[1].ListenAddr != "127.0.0.1:9554" {
		t.Fatalf("recorded addresses were not kept: %#v", registry.Cameras)
	}
}

// A camera added to the account is allocated around the ones already serving,
// never on top of them.
func TestBuildAllocatesANewCameraAroundTheRunningOnes(t *testing.T) {
	registry, err := Build(BuildOptions{
		BaseAddress: "127.0.0.1:8554",
		Credentials: []bridge.Credentials{
			{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"},
			{DeviceID: 2, DeviceUDID: "b", DeviceName: "Room B"},
		},
		// Camera b was allocated the base port by an earlier run; a is new.
		Recorded:  map[string]string{"b": "127.0.0.1:8554"},
		PortInUse: func(_ string, port int) bool { return port == 8554 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Cameras[1].ListenAddr != "127.0.0.1:8554" {
		t.Fatalf("the recorded camera moved: %q", registry.Cameras[1].ListenAddr)
	}
	if registry.Cameras[0].ListenAddr != "127.0.0.1:8555" {
		t.Fatalf("the new camera got %q, want the first free port above the base", registry.Cameras[0].ListenAddr)
	}
}

// A recorded address for another host is not this run's to keep: the base
// address is where the bridge is being told to listen now.
func TestBuildIgnoresARecordedAddressOnAnotherHost(t *testing.T) {
	registry, err := Build(BuildOptions{
		BaseAddress: "127.0.0.1:8554",
		Credentials: []bridge.Credentials{{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"}},
		Recorded:    map[string]string{"a": "10.0.0.5:8554", "b": "nonsense"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Cameras[0].ListenAddr != "127.0.0.1:8554" {
		t.Fatalf("listen address = %q", registry.Cameras[0].ListenAddr)
	}
}

// Without a probe nothing is derived from the network, which is what the bridge
// relies on: it must arrive at the addresses the media server was configured
// with, and on a reload its own listeners would read as busy.
func TestBuildRegistryNeverProbes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := BuildRegistry("127.0.0.1:"+portText,
		[]bridge.Credentials{{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"}})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Cameras[0].ListenAddr != listener.Addr().String() {
		t.Fatalf("listen address = %q, want the base address unchanged", registry.Cameras[0].ListenAddr)
	}
}

func TestPortInUseReportsABoundPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if !PortInUse("127.0.0.1", port) {
		t.Fatal("a bound port was reported free")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if PortInUse("127.0.0.1", port) {
		t.Fatal("a closed port was reported busy")
	}
}

// The search has to end: a registry that cannot be given ports says so instead
// of running off the end of the port range.
func TestBuildReportsWhenNoPortIsFree(t *testing.T) {
	_, err := Build(BuildOptions{
		BaseAddress: "127.0.0.1:65534",
		Credentials: []bridge.Credentials{{DeviceID: 1, DeviceUDID: "a", DeviceName: "Room A"}},
		PortInUse:   func(string, int) bool { return true },
	})
	if err == nil {
		t.Fatal("a registry with no free port at all was accepted")
	}
}
