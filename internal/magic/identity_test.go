package magic

import "testing"

func TestGenerateMagicUUIDIndependentSyntheticVector(t *testing.T) {
	// Expected value was generated independently with Python's hmac module.
	const want = "00123456sidtok5cc32121b117aaec78c5e853e153cbcb4ff3f3194bdd052ef2d0db46482e1154"
	got, err := GenerateMagicUUID(
		0x123456,
		"SID012345678901234567890123",
		"TOK012345678901234567890123",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("magic UUID mismatch: %q", got)
	}
	if len(got) != 78 {
		t.Fatalf("captured VM65 v1 shape is 78 bytes, got %d", len(got))
	}
}

func TestGenerateMagicUUIDRejectsUnsafeInputs(t *testing.T) {
	for _, test := range []struct{ sid, token string }{
		{"ab", "token"},
		{"SID with space", "token"},
		{"valid", "to\x00ken"},
	} {
		if _, err := GenerateMagicUUID(1, test.sid, test.token); err == nil {
			t.Fatalf("expected validation error for sid=%q token=%q", test.sid, test.token)
		}
	}
}
