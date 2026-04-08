package middleware

import "testing"

// TestIsPrivateClientIP 验证对应场景。
func TestIsPrivateClientIP(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "loopback", ip: "127.0.0.1", want: true},
		{name: "private-10", ip: "10.10.1.2", want: true},
		{name: "private-172", ip: "172.16.8.9", want: true},
		{name: "private-192", ip: "192.168.1.100", want: true},
		{name: "public", ip: "8.8.8.8", want: false},
		{name: "public-with-port", ip: "8.8.8.8:443", want: false},
		{name: "private-with-port", ip: "10.0.0.2:8080", want: true},
	}
	for _, tc := range cases {
		if got := isPrivateClientIP(tc.ip); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}
