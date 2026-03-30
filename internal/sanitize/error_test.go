package sanitize

import "testing"

func TestSMTPErrorFailureModes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "timeout", in: "dial tcp 1.2.3.4:25: i/o timeout", want: "smtp timeout"},
		{name: "refused", in: "dial tcp 1.2.3.4:25: connection refused", want: "smtp connection refused"},
		{name: "auth", in: "535 Authentication failed: bad password", want: "smtp authentication failed"},
		{name: "provider error pass through", in: "451 temporary provider error", want: "451 temporary provider error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SMTPError(tt.in); got != tt.want {
				t.Fatalf("SMTPError(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
