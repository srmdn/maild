package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestVerifyOK(t *testing.T) {
	v := NewVerifier("topsecret", 5*time.Minute)
	body := []byte(`{"type":"bounce","email":"user@example.com"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := "1700000000"
	sig := signHeader("topsecret", ts, body)

	err := v.Verify(ts, sig, body, now)
	if err != nil {
		t.Fatalf("expected signature to verify, got error: %v", err)
	}
}

func TestVerifyBadSignature(t *testing.T) {
	v := NewVerifier("topsecret", 5*time.Minute)
	body := []byte(`{"type":"bounce"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := "1700000000"

	err := v.Verify(ts, "v1=deadbeef", body, now)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got: %v", err)
	}
}

func TestVerifyOldTimestamp(t *testing.T) {
	v := NewVerifier("topsecret", 5*time.Minute)
	body := []byte(`{"type":"bounce"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := "1699999000"
	sig := signHeader("topsecret", ts, body)

	err := v.Verify(ts, sig, body, now)
	if !errors.Is(err, ErrTimestampOutsideTolerance) {
		t.Fatalf("expected ErrTimestampOutsideTolerance, got: %v", err)
	}
}

func TestVerifyInvalidTimestamp(t *testing.T) {
	v := NewVerifier("topsecret", 5*time.Minute)
	err := v.Verify("not-a-unix", "v1=deadbeef", []byte("{}"), time.Unix(1_700_000_000, 0))
	if !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("expected ErrInvalidTimestamp, got: %v", err)
	}
}

func TestVerifyCommaSignatureHeader(t *testing.T) {
	v := NewVerifier("topsecret", 5*time.Minute)
	body := []byte(`{"type":"unsubscribe"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := "1700000000"
	valid := signHeader("topsecret", ts, body)
	mixed := "v0=0011," + valid

	err := v.Verify(ts, mixed, body, now)
	if err != nil {
		t.Fatalf("expected signature to verify from comma-delimited header, got: %v", err)
	}
}

func signHeader(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}
