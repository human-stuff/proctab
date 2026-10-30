package netstat

import "testing"

func TestDecodeAddr(t *testing.T) {
	cases := []struct {
		in   string
		addr string
		port uint16
	}{
		{"0100007F:0016", "127.0.0.1", 22},
		{"00000000:0050", "0.0.0.0", 80},
		{"00000000000000000000000001000000:1F90", "::1", 8080},
		{"0000000000000000FFFF00000100007F:0035", "127.0.0.1", 53}, // v4-mapped
		{"000080FE00000000FF005450B6AC12FE:0222", "fe80::5054:ff:fe12:acb6", 546},
	}
	for _, c := range cases {
		a, p := decodeAddr(c.in)
		if a.String() != c.addr || p != c.port {
			t.Errorf("decodeAddr(%q) = %s:%d, want %s:%d", c.in, a, p, c.addr, c.port)
		}
	}
}

func TestListening(t *testing.T) {
	if !(Socket{Proto: "tcp", State: "LISTEN"}).Listening() {
		t.Error("tcp LISTEN should be listening")
	}
	if !(Socket{Proto: "udp", State: "CLOSE"}).Listening() {
		t.Error("bound udp should be listening")
	}
	if (Socket{Proto: "udp", State: "ESTABLISHED", RemotePort: 53}).Listening() {
		t.Error("connected udp is not listening")
	}
}
