package magic

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// GenerateMagicUUID reproduces libdevconn.so generate_sid_v1. Despite the
// native function name, the result is the 78-byte magicUuid used in the WEB2
// relay-open frame; it is not the camera SID itself.
func GenerateMagicUUID(deviceID uint32, sid, deviceToken string) (string, error) {
	if err := validateDerivationInput("SID", sid); err != nil {
		return "", err
	}
	if err := validateDerivationInput("device token", deviceToken); err != nil {
		return "", err
	}

	// Native format: "%08x%-20s%-27s%-20s". Widths are minimum widths.
	seed := []byte(fmt.Sprintf("%08x%-20s%-27s%-20s", deviceID, sid, deviceToken, sid))
	if len(seed) < sha256.Size {
		return "", errors.New("native HMAC seed unexpectedly shorter than 32 bytes")
	}
	mac := hmac.New(sha256.New, seed[:sha256.Size])
	_, _ = mac.Write([]byte(sid))
	digest := mac.Sum(nil)

	result := fmt.Sprintf("%08x%s%s%s", deviceID, sid[:3], deviceToken[:3], hex.EncodeToString(digest))
	return strings.ToLower(result), nil
}

func validateDerivationInput(name, value string) error {
	if len(value) < 3 {
		return fmt.Errorf("%s must contain at least three bytes", name)
	}
	for _, b := range []byte(value) {
		if b < 0x21 || b > 0x7e {
			return fmt.Errorf("%s must contain printable non-whitespace ASCII", name)
		}
	}
	return nil
}
