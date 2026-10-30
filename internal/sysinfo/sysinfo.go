// Package sysinfo reads system-wide stats from /proc and /sys.
package sysinfo

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type CPU struct {
	Name           string
	User, Sys, Low float64 // percents (htop: green, red, blue)
	Total          float64
}

type Mem struct {
	TotalKB, UsedKB, BuffersKB, CachedKB, AvailKB uint64
	SwapTotalKB, SwapUsedKB                       uint64
}

type Disk struct {
	Device, Mount, FSType string
	Total, Used, Avail    uint64
}

type NetIf struct {
	Name           string
	RxBytes        uint64
	TxBytes        uint64
	RxRate, TxRate float64 // bytes/s
	Addrs          []string
	Up             bool
}

type Static struct {
	Hostname, OS, Kernel, Arch, CPUModel string
	Cores                                int
}

// Snapshot is the dynamic system state.
type Snapshot struct {
	CPUs    []CPU // CPUs[0] is the aggregate
	Mem     Mem
	Load    [3]float64
	Running int
	Tasks   int
	Uptime  time.Duration
	Disks   []Disk
	Nets    []NetIf
}

type cpuTicks struct{ user, nice, sys, idle, total uint64 }

// Sampler keeps previous counters for rate computation.
type Sampler struct {
	mu      sync.Mutex
	prevCPU map[string]cpuTicks
	prevNet map[string]NetIf
	prevAt  time.Time
}

func NewSampler() *Sampler {
	return &Sampler{prevCPU: map[string]cpuTicks{}, prevNet: map[string]NetIf{}}
}

func (s *Sampler) Sample(withDisks bool) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var snap Snapshot
	snap.CPUs = s.cpus()
	snap.Mem = ReadMem()
	snap.Load, snap.Running, snap.Tasks = loadavg()
	snap.Uptime = uptime()
	if withDisks {
		snap.Disks = Disks()
	}
	snap.Nets = s.nets(now)
	s.prevAt = now
	return snap
}

func (s *Sampler) cpus() []CPU {
	b, _ := os.ReadFile("/proc/stat")
	var out []CPU
	next := map[string]cpuTicks{}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu") {
			break
		}
		f := strings.Fields(line)
		if len(f) < 9 {
			continue
		}
		v := make([]uint64, 8)
		for i := range v {
			v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
		}
		// user nice system idle iowait irq softirq steal
		t := cpuTicks{user: v[0], nice: v[1], sys: v[2] + v[5] + v[6], idle: v[3] + v[4]}
		for _, x := range v {
			t.total += x
		}
		next[f[0]] = t
		c := CPU{Name: f[0]}
		if p, ok := s.prevCPU[f[0]]; ok && t.total > p.total {
			d := float64(t.total - p.total)
			c.User = float64(t.user-p.user) / d * 100
			c.Low = float64(t.nice-p.nice) / d * 100
			c.Sys = float64(t.sys-p.sys) / d * 100
			c.Total = 100 - float64(t.idle-p.idle)/d*100
		}
		out = append(out, c)
	}
	s.prevCPU = next
	return out
}

func ReadMem() Mem {
	m := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Mem{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
		m[k] = n
	}
	avail, ok := m["MemAvailable"]
	if !ok {
		avail = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	cached := m["Cached"] + m["SReclaimable"] - m["Shmem"]
	used := m["MemTotal"] - avail
	return Mem{
		TotalKB: m["MemTotal"], UsedKB: used, BuffersKB: m["Buffers"], CachedKB: cached,
		AvailKB: avail, SwapTotalKB: m["SwapTotal"], SwapUsedKB: m["SwapTotal"] - m["SwapFree"],
	}
}

func loadavg() (l [3]float64, running, total int) {
	b, _ := os.ReadFile("/proc/loadavg")
	f := strings.Fields(string(b))
	if len(f) < 4 {
		return
	}
	for i := 0; i < 3; i++ {
		l[i], _ = strconv.ParseFloat(f[i], 64)
	}
	r, t, _ := strings.Cut(f[3], "/")
	running, _ = strconv.Atoi(r)
	total, _ = strconv.Atoi(t)
	return
}

func uptime() time.Duration {
	b, _ := os.ReadFile("/proc/uptime")
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	s, _ := strconv.ParseFloat(f[0], 64)
	return time.Duration(s * float64(time.Second))
}

var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true,
	"vfat": true, "exfat": true, "f2fs": true, "ntfs": true, "ntfs3": true, "fuseblk": true,
	"nfs": true, "nfs4": true, "cifs": true,
}

// Disks lists real mounted filesystems (dedup by device).
func Disks() []Disk {
	b, _ := os.ReadFile("/proc/mounts")
	seen := map[string]bool{}
	var out []Disk
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		mnt := strings.ReplaceAll(f[1], `\040`, " ")
		d := Disk{Device: f[0], Mount: mnt, FSType: f[2]}
		done := make(chan struct{})
		go func() {
			var st syscall.Statfs_t
			if syscall.Statfs(mnt, &st) == nil {
				bs := uint64(st.Bsize)
				d.Total = st.Blocks * bs
				d.Used = (st.Blocks - st.Bfree) * bs
				d.Avail = st.Bavail * bs
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(300 * time.Millisecond): // dead network mount
		}
		out = append(out, d)
	}
	return out
}

func (s *Sampler) nets(now time.Time) []NetIf {
	b, _ := os.ReadFile("/proc/net/dev")
	lines := strings.Split(string(b), "\n")
	dt := now.Sub(s.prevAt).Seconds()
	next := map[string]NetIf{}
	var out []NetIf
	ifaces, _ := net.Interfaces()
	ifm := map[string]net.Interface{}
	for _, i := range ifaces {
		ifm[i.Name] = i
	}
	for _, line := range lines[min(2, len(lines)):] {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		n := NetIf{Name: name}
		n.RxBytes, _ = strconv.ParseUint(f[0], 10, 64)
		n.TxBytes, _ = strconv.ParseUint(f[8], 10, 64)
		if p, ok := s.prevNet[name]; ok && dt > 0 && n.RxBytes >= p.RxBytes && n.TxBytes >= p.TxBytes {
			n.RxRate = float64(n.RxBytes-p.RxBytes) / dt
			n.TxRate = float64(n.TxBytes-p.TxBytes) / dt
		}
		if i, ok := ifm[name]; ok {
			n.Up = i.Flags&net.FlagUp != 0
			addrs, _ := i.Addrs()
			for _, a := range addrs {
				n.Addrs = append(n.Addrs, a.String())
			}
		}
		next[name] = n
		out = append(out, n)
	}
	s.prevNet = next
	return out
}

// ReadStatic reads values that do not change while running.
func ReadStatic() Static {
	st := Static{}
	st.Hostname, _ = os.Hostname()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		st.Kernel = strings.TrimSpace(string(b))
	}
	// Utsname field types differ per GOARCH, so avoid syscall.Uname.
	if b, err := os.ReadFile("/proc/sys/kernel/arch"); err == nil {
		st.Arch = strings.TrimSpace(string(b))
	} else {
		st.Arch = runtime.GOARCH
	}
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				st.OS = strings.Trim(v, `"'`)
			}
		}
		break
	}
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "processor" {
			st.Cores++
		}
		if st.CPUModel == "" && (k == "model name" || k == "Hardware" || k == "Model") {
			st.CPUModel = strings.TrimSpace(v)
		}
	}
	return st
}
