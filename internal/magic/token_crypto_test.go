package magic

import (
	"bytes"
	"encoding/hex"
	"testing"
)

const syntheticDeviceToken = "TOK012345678901234567890123"
const syntheticPlainRTSP = "OPTIONS rtsp://127.0.0.1:16667/test RTSP/1.0\r\n\r\n"
const syntheticCipherHex = "15161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f64aefafce3e5e6f2a2da8ad99dd0ce85dbbdc3d5cbd0d086dbb5d19e9ec0ccc2808ffb8f88fafcdbfa85d086da82a2edf1"

func TestTokenDecoderIndependentSyntheticVector(t *testing.T) {
	cipher, err := hex.DecodeString(syntheticCipherHex)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewTokenDecoder(syntheticDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the native behavior where bootstrap bytes span TCP reads.
	var decoded []byte
	for _, part := range [][]byte{cipher[:7], cipher[7:28], cipher[28:41], cipher[41:]} {
		plain, decodeErr := decoder.Decode(part)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		decoded = append(decoded, plain...)
	}
	if string(decoded) != syntheticPlainRTSP {
		t.Fatalf("decoded plaintext mismatch: %q", decoded)
	}
}

func TestTokenEncoderIndependentSyntheticVector(t *testing.T) {
	// Bytes 0..26 map to native-valid prefix bytes 0x15..0x2f. Byte 72
	// maps to marker 100 for this 27-byte token.
	randomBytes := make([]byte, 0, 28)
	for value := byte(0); value < 27; value++ {
		randomBytes = append(randomBytes, value)
	}
	randomBytes = append(randomBytes, 72)
	encoder, err := newTokenEncoder(syntheticDeviceToken, bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encoder.Encode([]byte(syntheticPlainRTSP))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != syntheticCipherHex {
		t.Fatalf("encoded vector mismatch: %x", got)
	}
}

func TestTokenCryptoContinuesStateWithoutSecondBootstrap(t *testing.T) {
	randomBytes := make([]byte, 28)
	encoder, err := newTokenEncoder(syntheticDeviceToken, bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewTokenDecoder(syntheticDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range [][]byte{[]byte("first"), []byte("second packet")} {
		cipher, encodeErr := encoder.Encode(message)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		plain, decodeErr := decoder.Decode(cipher)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if !bytes.Equal(plain, message) {
			t.Fatalf("round-trip mismatch: %q != %q", plain, message)
		}
	}
}
