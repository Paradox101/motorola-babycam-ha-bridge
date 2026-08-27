package bridge

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	b, err := New(Config{ListenAddr: "127.0.0.1:0", Credentials: testCreds()})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Listen(); err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	rec := httptest.NewRecorder()
	b.HealthHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status         string `json:"status"`
		SessionsTotal  int64  `json:"sessions_total"`
		SessionsActive int64  `json:"sessions_active"`
		Listen         string `json:"listen"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Listen == "" {
		t.Error("listen address should be reported once bound")
	}
}
