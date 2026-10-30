// Package netstat parses /proc/net/{tcp,tcp6,udp,udp6}.
package netstat

import (
	"bufio"
	"encoding/hex"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
)

var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1",
	"05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT",
	"09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING", "0C": "NEW_SYN_RECV",
}

// Socket is one row of /proc/net/*.
type Socket struct {
	Proto      string // tcp, tcp6, udp, udp6
	LocalAddr  netip.Addr
	LocalPort  uint16
	RemoteAddr netip.Addr
	RemotePort uint16
	State      string
	UID        int
	Inode      uint64
	PID        int // 0 if unknown (permission)
}

// Listening reports whether the socket is a server socket.
func (s Socket) Listening() bool {
	if strings.HasPrefix(s.Proto, "tcp") {
		return s.State == "LISTEN"
	}
	// bound, unconnected UDP
	return s.State == "CLOSE" && s.RemotePort == 0
}

// Read returns all sockets for the given /proc root (normally "/proc").
func Read(root string) []Socket {
	var out []Socket
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		out = append(out, readFile(root+"/net/"+proto, proto)...)
	}
	return out
}

func readFile(path, proto string) []Socket {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Socket
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 10 {
			continue
		}
		la, lp := decodeAddr(fs[1])
		ra, rp := decodeAddr(fs[2])
		uid, _ := strconv.Atoi(fs[7])
		ino, _ := strconv.ParseUint(fs[9], 10, 64)
		out = append(out, Socket{
			Proto: proto, LocalAddr: la, LocalPort: lp, RemoteAddr: ra, RemotePort: rp,
			State: tcpStates[fs[3]], UID: uid, Inode: ino,
		})
	}
	return out
}

// decodeAddr decodes "0100007F:0016". Each 32-bit word is host (little) endian.
func decodeAddr(s string) (netip.Addr, uint16) {
	h, p, _ := strings.Cut(s, ":")
	port, _ := strconv.ParseUint(p, 16, 16)
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.Addr{}, uint16(port)
	}
	for i := 0; i+3 < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	a, _ := netip.AddrFromSlice(b)
	return a.Unmap(), uint16(port)
}

// Listener groups listening sockets by (proto family, port, pid).
type Listener struct {
	Proto string // TCP / UDP
	Port  uint16
	Addrs []string
	PID   int
	UID   int
	Inode uint64
}

// Listeners returns listening sockets, de-duplicated across v4/v6, sorted by port.
func Listeners(socks []Socket, inodePID map[uint64]int) []Listener {
	type key struct {
		proto string
		port  uint16
		pid   int
	}
	idx := map[key]*Listener{}
	var order []key
	for _, s := range socks {
		if !s.Listening() {
			continue
		}
		pid := inodePID[s.Inode]
		proto := strings.ToUpper(strings.TrimSuffix(s.Proto, "6"))
		k := key{proto, s.LocalPort, pid}
		l, ok := idx[k]
		if !ok {
			l = &Listener{Proto: proto, Port: s.LocalPort, PID: pid, UID: s.UID, Inode: s.Inode}
			idx[k] = l
			order = append(order, k)
		}
		addr := s.LocalAddr.String()
		if s.LocalAddr.IsUnspecified() {
			addr = "*"
			if s.Proto == "tcp6" || s.Proto == "udp6" {
				addr = "[::]"
			}
		}
		if !contains(l.Addrs, addr) {
			l.Addrs = append(l.Addrs, addr)
		}
	}
	out := make([]Listener, 0, len(order))
	for _, k := range order {
		out = append(out, *idx[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
