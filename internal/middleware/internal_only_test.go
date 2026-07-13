package middleware

import "testing"

// TestIsInternalClientAddr 验证仅回环和私网地址属于内网来源。
func TestIsInternalClientAddr(t *testing.T) {
	cases := []struct {
		name string // name 表示测试场景名称。
		ip   string // ip 表示测试字段。
		want bool   // want 表示期望结果。
	}{
		{name: "loopback", ip: "127.0.0.1", want: true},
		{name: "private-10", ip: "10.10.1.2", want: true},
		{name: "private-172", ip: "172.16.8.9", want: true},
		{name: "private-192", ip: "192.168.1.100", want: true},
		{name: "public", ip: "8.8.8.8", want: false},
		{name: "public-with-port", ip: "8.8.8.8:443", want: false},
		{name: "private-with-port", ip: "10.0.0.2:8080", want: true},
		{name: "ipv4-link-local", ip: "169.254.1.1", want: false},
		{name: "ipv6-loopback", ip: "::1", want: true},
		{name: "ipv6-private", ip: "fd00::1", want: true},
		{name: "ipv6-private-with-port", ip: "[fd00::1]:8080", want: true},
		{name: "ipv6-link-local", ip: "fe80::1", want: false},
		{name: "ipv6-public", ip: "2001:4860:4860::8888", want: false},
		{name: "empty", ip: "", want: false},
		{name: "invalid", ip: "not-an-ip", want: false},
	}
	for _, tc := range cases {
		addr, ok := parseAddrValue(tc.ip)
		got := ok && isInternalClientAddr(addr)
		if got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}
