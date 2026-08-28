package magic

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const maxDeviceTokenLength = 47 // native connection field is 48 bytes including NUL

// TokenEncoder implements the stateful WEB2 byte transformation used from
// the local RTSP socket toward the relay. One encoder is required per TCP
// direction and must not be shared across sessions.
type TokenEncoder struct {
	key          []byte
	state        int
	bootstrapped bool
	random       io.Reader
}

// TokenDecoder implements the inverse relay-to-local transformation. It
// tolerates a bootstrap split over multiple TCP reads, as the native receiver
// does.
type TokenDecoder struct {
	key       []byte
	state     int
	bootstrap []byte
	ready     bool
}

func NewTokenEncoder(deviceToken string) (*TokenEncoder, error) {
	return newTokenEncoder(deviceToken, rand.Reader)
}

func newTokenEncoder(deviceToken string, random io.Reader) (*TokenEncoder, error) {
	key, err := tokenKey(deviceToken)
	if err != nil {
		return nil, err
	}
	if random == nil {
		return nil, errors.New("random source is required")
	}
	return &TokenEncoder{key: key, random: random}, nil
}

func NewTokenDecoder(deviceToken string) (*TokenDecoder, error) {
	key, err := tokenKey(deviceToken)
	if err != nil {
		return nil, err
	}
	return &TokenDecoder{key: key, bootstrap: make([]byte, 0, len(key)+1)}, nil
}

func (encoder *TokenEncoder) Encode(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	result := make([]byte, 0, len(plain)+len(encoder.key)+1)
	if !encoder.bootstrapped {
		prefix := make([]byte, len(encoder.key))
		for index := range prefix {
			value, err := randomRange(encoder.random, 0x15, 0xff)
			if err != nil {
				return nil, fmt.Errorf("generate crypto prefix: %w", err)
			}
			prefix[index] = value
		}
		marker, err := randomRange(encoder.random, byte(len(encoder.key)+1), 0xff)
		if err != nil {
			return nil, fmt.Errorf("generate crypto state marker: %w", err)
		}
		applyBootstrap(encoder.key, prefix)
		encoder.state = int(marker) % len(encoder.key)
		encoder.bootstrapped = true
		result = append(result, prefix...)
		result = append(result, marker)
	}
	result = append(result, transformEncode(encoder.key, &encoder.state, plain)...)
	return result, nil
}

func (decoder *TokenDecoder) Decode(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, nil
	}
	if !decoder.ready {
		need := len(decoder.key) + 1 - len(decoder.bootstrap)
		take := need
		if take > len(cipher) {
			take = len(cipher)
		}
		decoder.bootstrap = append(decoder.bootstrap, cipher[:take]...)
		cipher = cipher[take:]
		if len(decoder.bootstrap) < len(decoder.key)+1 {
			return nil, nil
		}
		applyBootstrap(decoder.key, decoder.bootstrap[:len(decoder.key)])
		decoder.state = int(decoder.bootstrap[len(decoder.key)]) % len(decoder.key)
		decoder.bootstrap = nil
		decoder.ready = true
	}
	return transformDecode(decoder.key, &decoder.state, cipher), nil
}

func transformEncode(key []byte, state *int, plain []byte) []byte {
	cipher := make([]byte, len(plain))
	for index, value := range plain {
		cipher[index] = value ^ key[*state]
		*state = (((int(key[*state]) + int(value)) | 1) + *state) % len(key)
	}
	return cipher
}

func transformDecode(key []byte, state *int, cipher []byte) []byte {
	plain := make([]byte, len(cipher))
	for index, value := range cipher {
		plain[index] = value ^ key[*state]
		*state = (((int(key[*state]) + int(plain[index])) | 1) + *state) % len(key)
	}
	return plain
}

func applyBootstrap(key, prefix []byte) {
	rolling := byte(0xaa)
	for index, randomByte := range prefix {
		rolling ^= (key[index] >> 1) ^ ((randomByte & 0x7f) << 1)
		key[index] = rolling
	}
}

func tokenKey(deviceToken string) ([]byte, error) {
	if len(deviceToken) < 1 || len(deviceToken) > maxDeviceTokenLength {
		return nil, fmt.Errorf("device token length must be 1..%d bytes", maxDeviceTokenLength)
	}
	for _, value := range []byte(deviceToken) {
		if value < 0x21 || value > 0x7e {
			return nil, errors.New("device token must contain printable non-whitespace ASCII")
		}
	}
	return append([]byte(nil), deviceToken...), nil
}

func randomRange(source io.Reader, minimum, maximum byte) (byte, error) {
	if minimum > maximum {
		return 0, errors.New("invalid random range")
	}
	var value [1]byte
	if _, err := io.ReadFull(source, value[:]); err != nil {
		return 0, err
	}
	span := int(maximum) - int(minimum) + 1
	return byte(int(minimum) + int(value[0])%span), nil
}
