// Package devicecontrol implements the TLS protocol used for camera control.
package devicecontrol

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const temperatureCapability = "temperature_reading"

func AuthenticationCommand(deviceID uint32, token string) (string, error) {
	if deviceID == 0 || token == "" || strings.ContainsAny(token, " \r\n\t") {
		return "", errors.New("device control authentication requires a device id and token")
	}
	return fmt.Sprintf("app %d %s\n", deviceID, token), nil
}

func ParseAuthentication(line string) error {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "app" {
		return errors.New("unexpected device control authentication response")
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || status <= 0 {
		return errors.New("device control authentication rejected")
	}
	return nil
}

func ParseTemperatureCapability(line string) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "caplist" {
		return false, errors.New("unexpected capability response")
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 0 || len(fields) != 2+count*5 {
		return false, errors.New("malformed capability response")
	}
	for index := 0; index < count; index++ {
		if fields[2+index*5] == temperatureCapability {
			return true, nil
		}
	}
	return false, nil
}

func ParseTemperature(line string) (float64, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || fields[0] != "get" || fields[1] != "1" || fields[2] != temperatureCapability {
		return 0, errors.New("unexpected temperature response")
	}
	raw, err := strconv.Atoi(fields[3])
	if err != nil || raw <= 0 || raw >= 500 {
		return 0, errors.New("temperature reading is out of range")
	}
	return float64(raw) / 10, nil
}
