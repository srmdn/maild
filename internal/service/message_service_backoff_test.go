package service

import (
	"testing"
	"time"
)

func TestBackoffDurationBounded(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 1 * time.Second},
		{attempt: 1, want: 1 * time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 6, want: 32 * time.Second},
		{attempt: 10, want: 32 * time.Second},
	}

	for _, tt := range tests {
		if got := backoffDuration(tt.attempt); got != tt.want {
			t.Fatalf("backoffDuration(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestWebhookBackoffDurationBounded(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 1 * time.Second},
		{attempt: 1, want: 1 * time.Second},
		{attempt: 2, want: 2 * time.Second},
		{attempt: 3, want: 4 * time.Second},
		{attempt: 4, want: 8 * time.Second},
		{attempt: 8, want: 8 * time.Second},
	}

	for _, tt := range tests {
		if got := webhookBackoffDuration(tt.attempt); got != tt.want {
			t.Fatalf("webhookBackoffDuration(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}
