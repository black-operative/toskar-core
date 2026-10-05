package auth

import (
	"net/http/httptest"
	"testing"
)

func TestFromThisComputer(t *testing.T) {
	for _, c := range []struct {
		remote, header string
		want           bool
	}{
		{"127.0.0.1:50000", "", true},
		{"[::1]:50000", "", true},
		{"[::ffff:127.0.0.1]:50000", "", true},
		{"192.168.1.20:50000", "", false},
		{"[fe80::1]:50000", "", false},
		{"127.0.0.1:50000", "X-Forwarded-For", false},
		{"127.0.0.1:50000", "Forwarded", false},
		{"127.0.0.1:50000", "X-Real-IP", false},
		{"not an address", "", false},
	} {
		r := httptest.NewRequest(httpMethod, "/api/v1/health", nil)
		r.RemoteAddr = c.remote
		if c.header != "" {
			r.Header.Set(c.header, "192.168.1.20")
		}
		if got := FromThisComputer(r); got != c.want {
			t.Errorf("%s with %q: got %v, want %v", c.remote, c.header, got, c.want)
		}
	}
}
