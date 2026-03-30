package sanitize

import "strings"

func SMTPError(raw string) string {
	out := strings.TrimSpace(raw)
	if out == "" {
		return "smtp delivery failed"
	}

	low := strings.ToLower(out)
	if strings.Contains(low, "password") ||
		strings.Contains(low, "auth") ||
		strings.Contains(low, "credential") ||
		strings.Contains(low, "plainauth") {
		return "smtp authentication failed"
	}
	if len(out) > 240 {
		return out[:240]
	}
	return out
}

func HTTPInternalError(_ error) string {
	return "internal server error"
}
