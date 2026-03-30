package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingTimestamp          = errors.New("missing webhook timestamp")
	ErrMissingSignature          = errors.New("missing webhook signature")
	ErrInvalidTimestamp          = errors.New("invalid webhook timestamp")
	ErrTimestampOutsideTolerance = errors.New("webhook timestamp outside tolerance")
	ErrInvalidSignatureFormat    = errors.New("invalid webhook signature format")
	ErrSignatureMismatch         = errors.New("webhook signature mismatch")
)

type Verifier struct {
	secret  []byte
	maxSkew time.Duration
}

func NewVerifier(secret string, maxSkew time.Duration) *Verifier {
	return &Verifier{
		secret:  []byte(secret),
		maxSkew: maxSkew,
	}
}

func (v *Verifier) Verify(timestampHeader, signatureHeader string, body []byte, now time.Time) error {
	timestampHeader = strings.TrimSpace(timestampHeader)
	if timestampHeader == "" {
		return ErrMissingTimestamp
	}

	parsedTs, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return ErrInvalidTimestamp
	}

	sig, err := parseSignature(signatureHeader)
	if err != nil {
		return err
	}

	ts := time.Unix(parsedTs, 0)
	if now.Sub(ts) > v.maxSkew || ts.Sub(now) > v.maxSkew {
		return ErrTimestampOutsideTolerance
	}

	expected := sign(v.secret, timestampHeader, body)
	if subtle.ConstantTimeCompare(sig, expected) != 1 {
		return ErrSignatureMismatch
	}

	return nil
}

func sign(secret []byte, timestamp string, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

func parseSignature(header string) ([]byte, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, ErrMissingSignature
	}

	parts := strings.Split(header, ",")
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "v1=") {
			token = strings.TrimPrefix(token, "v1=")
		}
		decoded, err := hex.DecodeString(token)
		if err == nil && len(decoded) > 0 {
			return decoded, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrInvalidSignatureFormat, "expected hex digest or v1=<hex>")
}
