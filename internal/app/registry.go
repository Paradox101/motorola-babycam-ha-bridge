package app

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
)

type Camera struct {
	Credentials bridge.Credentials
	StreamName  string
	ListenAddr  string
}

type Registry struct {
	Cameras      []Camera
	LegacyAlias  string
	LegacyTarget string
}

// BuildOptions describes how one registry is built.
//
// Two things decide a camera's listen address, in this order: the address an
// earlier run already gave it, and — for a camera that has none — the first free
// port at or above the one its position implies. Which of the two applies is
// what keeps the media server and the bridge pointing at the same socket:
// nothing that already has an address ever moves, and a port somebody else
// holds is skipped instead of failing to bind.
type BuildOptions struct {
	// BaseAddress is the host and first port to derive addresses from.
	BaseAddress string
	Credentials []bridge.Credentials

	// Recorded maps a camera's UDID to the listen address an earlier run gave
	// it. A camera that has one keeps it, whatever the probe would say: that
	// address is in the media server's configuration, and on a credential
	// refresh the port is busy precisely because this add-on's own bridge is
	// serving on it.
	Recorded map[string]string

	// PortInUse reports whether a port cannot be listened on. Nil probes
	// nothing and derives every address from the base, which is what the bridge
	// does: it must arrive at the addresses the media server was configured
	// with, and on a reload its own listeners would read as busy. Only the
	// setup command, which writes both files, passes PortInUse.
	PortInUse func(host string, port int) bool
}

// BuildRegistry derives every address, without probing. It is the form the
// bridge uses, where the addresses come from the registry file rather than from
// a fresh allocation.
func BuildRegistry(baseAddress string, credentials []bridge.Credentials) (Registry, error) {
	return Build(BuildOptions{BaseAddress: baseAddress, Credentials: credentials})
}

// Build assembles the registry.
func Build(options BuildOptions) (Registry, error) {
	host, portText, err := net.SplitHostPort(options.BaseAddress)
	if err != nil {
		return Registry{}, err
	}
	basePort, err := strconv.Atoi(portText)
	if err != nil || basePort < 1 || basePort > 65535 {
		return Registry{}, errors.New("base bridge port must be between 1 and 65535")
	}
	ordered := append([]bridge.Credentials(nil), options.Credentials...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].DeviceUDID < ordered[j].DeviceUDID
	})
	inUse := options.PortInUse
	if inUse == nil {
		inUse = func(string, int) bool { return false }
	}

	// Addresses an earlier run recorded are claimed first, so a camera that is
	// still to be allocated cannot be handed a port one of them already holds.
	addresses := make([]string, len(ordered))
	taken := make(map[int]bool, len(ordered))
	for index, credential := range ordered {
		recorded, ok := recordedAddress(options.Recorded, credential, host)
		if !ok {
			continue
		}
		addresses[index] = recorded
		if _, port, err := net.SplitHostPort(recorded); err == nil {
			if number, err := strconv.Atoi(port); err == nil {
				taken[number] = true
			}
		}
	}

	registry := Registry{LegacyAlias: "vm65"}
	// Every name that has been handed out, not a count per base name: counting
	// alone hands "Nursery", "Nursery" and "Nursery 2" the names "nursery",
	// "nursery-2" and "nursery-2" — two cameras sharing one go2rtc stream, one
	// snapshot URL and one card on the page.
	usedNames := make(map[string]bool, len(ordered))
	for index, credential := range ordered {
		address := addresses[index]
		if address == "" {
			// The historical layout: the first camera on the base port, the
			// rest a thousand above it. It is only where the search starts.
			wanted := basePort
			if index > 0 {
				wanted = basePort + 999 + index
			}
			port, err := freePort(host, wanted, taken, inUse)
			if err != nil {
				return Registry{}, err
			}
			taken[port] = true
			address = net.JoinHostPort(host, strconv.Itoa(port))
		}
		nameSource := credential.DeviceName
		if nameSource == "" {
			nameSource = "camera-" + credential.DeviceUDID
		}
		baseName := streamName(nameSource)
		name := baseName
		for suffix := 2; usedNames[name]; suffix++ {
			name = baseName + "-" + strconv.Itoa(suffix)
		}
		usedNames[name] = true
		registry.Cameras = append(registry.Cameras, Camera{
			Credentials: credential,
			StreamName:  name,
			ListenAddr:  address,
		})
	}
	if len(registry.Cameras) > 0 {
		registry.LegacyTarget = registry.Cameras[0].StreamName
	}
	return registry, nil
}

// Addresses reports each camera's listen address by UDID, which is what a later
// run reads back as Recorded.
func (r Registry) Addresses() map[string]string {
	addresses := make(map[string]string, len(r.Cameras))
	for _, camera := range r.Cameras {
		if camera.Credentials.DeviceUDID != "" {
			addresses[camera.Credentials.DeviceUDID] = camera.ListenAddr
		}
	}
	return addresses
}

// recordedAddress returns the address an earlier run gave this camera, on this
// host. An address recorded for another host is ignored: the base address is
// where the bridge is being told to listen now.
func recordedAddress(recorded map[string]string, credential bridge.Credentials, host string) (string, bool) {
	if len(recorded) == 0 || credential.DeviceUDID == "" {
		return "", false
	}
	address, ok := recorded[credential.DeviceUDID]
	if !ok {
		return "", false
	}
	recordedHost, portText, err := net.SplitHostPort(address)
	if err != nil || recordedHost != host {
		return "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	return address, true
}

// freePort returns the first port at or above wanted that this registry has not
// already claimed and that nothing else is listening on.
func freePort(host string, wanted int, taken map[int]bool, inUse func(string, int) bool) (int, error) {
	for port := wanted; port <= 65535; port++ {
		if taken[port] || inUse(host, port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no free bridge port at or above %d", wanted)
}

// PortInUse reports whether a port cannot be bound right now. Binding it is the
// only honest test — connecting says nothing about a socket bound to another
// address on the same host — and the listener is closed again straight away, so
// this races nothing but a process that starts in that same instant. The bridge
// that binds for real a moment later is what settles that race, by failing and
// being restarted.
func PortInUse(host string, port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return true
	}
	_ = listener.Close()
	return false
}

func streamName(name string) string {
	var builder strings.Builder
	separator := false
	for _, character := range strings.ToLower(name) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			separator = false
			continue
		}
		if builder.Len() > 0 && !separator {
			builder.WriteByte('-')
			separator = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "camera"
	}
	return result
}
