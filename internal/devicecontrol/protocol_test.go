package devicecontrol

import "testing"

func TestAuthenticationCommandUsesCapturedWireFormat(t *testing.T) {
	got, err := AuthenticationCommand(42, "device-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "app 42 device-token\n" {
		t.Fatalf("command = %q", got)
	}
}

func TestParseTemperatureCapabilityFindsSupportedCamera(t *testing.T) {
	supported, err := ParseTemperatureCapability("caplist 2 temperature_reading r int 0 0 camera.volume rw int 5 0")
	if err != nil || !supported {
		t.Fatalf("supported=%t err=%v", supported, err)
	}
}

func TestParseTemperatureConvertsTenths(t *testing.T) {
	got, err := ParseTemperature("get 1 temperature_reading 214")
	if err != nil || got != 21.4 {
		t.Fatalf("temperature=%v err=%v", got, err)
	}
}

func TestProtocolRejectsMalformedAndOutOfRangeResponses(t *testing.T) {
	if err := ParseAuthentication("app 0 rejected"); err == nil {
		t.Fatal("rejected authentication was accepted")
	}
	if _, err := ParseTemperatureCapability("caplist 1 temperature_reading r int"); err == nil {
		t.Fatal("truncated capability list was accepted")
	}
	for _, line := range []string{
		"get 1 camera.volume 214",
		"get 1 temperature_reading 0",
		"get 1 temperature_reading 500",
	} {
		if _, err := ParseTemperature(line); err == nil {
			t.Fatalf("invalid temperature response accepted: %q", line)
		}
	}
}
