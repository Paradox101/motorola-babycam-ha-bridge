package fivegencare

import "testing"

func TestCommandsMatchAppWireOrder(t *testing.T) {
	otp, err := OTPCommand("uuid-1", "a@example.test")
	assertWire(t, otp, err, "v3_otp uuid-1 email a@example.test 6\n")
	login, err := LoginSetCommand(42, "uuid-1", "a@example.test", "123456")
	assertWire(t, login, err, "v3_loginset 42 uuid-1 email a@example.test 123456\n")
	session, err := SessionCommand(Session{UserID: 42, SessionToken: "token", SessionID: "sid"})
	assertWire(t, session, err, "v3_session 42 token sid\n")
}

func assertWire(t *testing.T, got []byte, err error, want string) {
	t.Helper()
	if err != nil || string(got) != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestParseLoginAndDeviceList(t *testing.T) {
	s, err := ParseLogin("v3_loginset 42 st mt session shard.moto.5gencare.com")
	if err != nil || s.UserID != 42 || s.SessionToken != "st" || s.SessionID != "session" {
		t.Fatalf("session: %+v, %v", s, err)
	}
	d, err := ParseDeviceList("v3_dlist 1 123 UDID VM65CONNECT Baby%20Room TOK SID NL")
	if err != nil || len(d) != 1 || d[0].ID != 123 || d[0].Name != "Baby Room" || d[0].DeviceToken != "TOK" {
		t.Fatalf("devices: %+v, %v", d, err)
	}
}

func TestOwnerAccessToken(t *testing.T) {
	if got, want := OwnerAccessToken("TOK"), "7855986aead8e60c48aa4a185d5b4099b7a684ea"; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestRejectMalformedDeviceList(t *testing.T) {
	if _, err := ParseDeviceList("v3_dlist 2 1 a b c d e f"); err == nil {
		t.Fatal("expected field-count error")
	}
}

func TestSessionResponseAcceptsPositiveAccountStatus(t *testing.T) {
	if err := validateSessionResponse("v3_session 42"); err != nil {
		t.Fatalf("positive account status must restore a session: %v", err)
	}
}

func TestSessionTokenCandidatesTrySessionThenMaster(t *testing.T) {
	s := Session{SessionToken: "session", MasterToken: "master"}
	got := sessionTokenCandidates(s)
	if len(got) != 2 || got[0] != "session" || got[1] != "master" {
		t.Fatalf("candidate order: %#v", got)
	}
}
