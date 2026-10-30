// Package proc reads process information directly from /proc.
package proc

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ClkTck is USER_HZ. It is 100 on practically every Linux build.
const ClkTck = 100

var pageSize = uint64(os.Getpagesize())

// Process is a single snapshot of one process.
type Process struct {
	PID      int
	PPID     int
	UID      int
	User     string
	Comm     string
	Cmdline  string
	State    byte
	Priority int
	Nice     int
	Threads  int
	VirtKB   uint64
	ResKB    uint64
	ShrKB    uint64
	CPU      float64 // percent of one core
	Mem      float64 // percent of total memory
	TimeTick uint64  // utime+stime in ticks
	Start    time.Time
	Kernel   bool // kernel thread (no cmdline, ppid 2 or pid 2)

	// Filled by tree building.
	Depth  int
	Prefix string
}

// Detail holds expensive per-process info, read only on demand.
type Detail struct {
	Process
	Exe       string
	Cwd       string
	Root      string
	Environ   []string
	FDCount   int
	Sockets   []uint64 // socket inodes
	Cgroup    string
	Container string // container id if detected
	Children  []int
	Parent    string
	Errors    []string
}

// sampleKey guards against PID reuse between samples.
type sampleKey struct {
	pid   int
	start int64
}

// Sampler keeps state between scans so it can compute CPU%.
type Sampler struct {
	mu        sync.Mutex
	prevTicks map[sampleKey]uint64
	prevTotal uint64
	cmdlines  map[sampleKey]string
	scans     int
	buf       []byte
	bootTime  time.Time
	numCPU    int
}

func NewSampler() *Sampler {
	return &Sampler{prevTicks: map[sampleKey]uint64{}, cmdlines: map[sampleKey]string{},
		buf: make([]byte, 4096), bootTime: BootTime(), numCPU: NumCPU()}
}

// Scan reads every process. memTotalKB is used for MEM%.
func (s *Sampler) Scan(memTotalKB uint64) []Process {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := totalCPUTicks()
	dTotal := float64(total - s.prevTotal)
	if s.prevTotal == 0 || dTotal <= 0 {
		dTotal = 0
	}
	// dTotal covers all cores; per-core delta:
	perCore := dTotal / float64(max(1, s.numCPU))

	entries, _ := os.ReadDir("/proc")
	out := make([]Process, 0, len(entries))
	next := make(map[sampleKey]uint64, len(entries))
	users := Users()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, ok := readProcessBuf(pid, s.bootTime, s.buf, s.cmdlines)
		if !ok {
			continue
		}
		p.User = users.Name(p.UID)
		key := sampleKey{pid, p.Start.UnixNano()}
		if prev, ok := s.prevTicks[key]; ok && perCore > 0 && p.TimeTick >= prev {
			p.CPU = float64(p.TimeTick-prev) / perCore * 100
		}
		if memTotalKB > 0 {
			p.Mem = float64(p.ResKB) / float64(memTotalKB) * 100
		}
		next[key] = p.TimeTick
		out = append(out, p)
	}
	s.prevTicks = next
	s.prevTotal = total
	// Drop exited processes; refresh everything now and then because some
	// daemons rewrite their title (nginx, postgres, sshd).
	s.scans++
	for k := range s.cmdlines {
		if _, ok := next[k]; !ok || s.scans%10 == 0 {
			delete(s.cmdlines, k)
		}
	}
	return out
}

// readSmall reads a small /proc file with raw syscalls into buf, avoiding
// os.File overhead (poller registration, fstat) on thousands of reads per tick.
func readSmall(path string, buf []byte) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	n := 0
	for n < len(buf) {
		m, err := syscall.Read(fd, buf[n:])
		if err != nil {
			return nil, err
		}
		if m == 0 {
			break
		}
		n += m
	}
	return buf[:n], nil
}

func readProcess(pid int, boot time.Time) (Process, bool) {
	var buf [1024]byte
	return readProcessBuf(pid, boot, buf[:], nil)
}

// readProcessBuf reads stat/statm/cmdline. cmdline is looked up in cache
// first (it rarely changes) when cache is non-nil.
func readProcessBuf(pid int, boot time.Time, buf []byte, cache map[sampleKey]string) (Process, bool) {
	base := "/proc/" + strconv.Itoa(pid)
	stat, err := readSmall(base+"/stat", buf)
	if err != nil {
		return Process{}, false
	}
	p := Process{PID: pid}
	// comm may contain spaces and parens: take the last ')'.
	l := bytes.IndexByte(stat, '(')
	r := bytes.LastIndexByte(stat, ')')
	if l < 0 || r < l {
		return Process{}, false
	}
	p.Comm = string(stat[l+1 : r])
	f := strings.Fields(string(stat[r+1:]))
	// f[0] is field 3 (state).
	if len(f) < 22 {
		return Process{}, false
	}
	p.State = f[0][0]
	p.PPID = atoi(f[1])
	utime, stime := atou(f[11]), atou(f[12])
	p.TimeTick = utime + stime
	p.Priority = atoi(f[15])
	p.Nice = atoi(f[16])
	p.Threads = atoi(f[17])
	p.Start = boot.Add(time.Duration(atou(f[19])) * time.Second / ClkTck)
	p.VirtKB = atou(f[20]) / 1024

	if statm, err := readSmall(base+"/statm", buf); err == nil {
		sf := strings.Fields(string(statm))
		if len(sf) >= 3 {
			p.ResKB = atou(sf[1]) * pageSize / 1024
			p.ShrKB = atou(sf[2]) * pageSize / 1024
		}
	}
	p.UID = dirUID(base)
	key := sampleKey{pid, p.Start.UnixNano()}
	if c, ok := cache[key]; ok {
		p.Cmdline = c
	} else {
		if cl, err := os.ReadFile(base + "/cmdline"); err == nil && len(cl) > 0 {
			p.Cmdline = strings.TrimSpace(string(bytes.ReplaceAll(cl, []byte{0}, []byte{' '})))
		}
		if cache != nil && p.Cmdline != "" {
			cache[key] = p.Cmdline
		}
	}
	p.Kernel = p.Cmdline == "" && (p.PPID == 2 || pid == 2)
	return p, true
}

// dirUID returns the owner of /proc/[pid], which is the process's effective uid.
func dirUID(dir string) int {
	fi, err := os.Stat(dir)
	if err != nil {
		return -1
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

func readUID(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Uid:") {
			fs := strings.Fields(line)
			if len(fs) > 1 {
				return atoi(fs[1])
			}
		}
	}
	return -1
}

// ReadDetail gathers the expensive info for a single PID.
func ReadDetail(pid int) (*Detail, error) {
	base := "/proc/" + strconv.Itoa(pid)
	p, ok := readProcess(pid, BootTime())
	if !ok {
		return nil, os.ErrNotExist
	}
	if uid := readUID(base + "/status"); uid >= 0 {
		p.UID = uid // real uid, as htop shows
	}
	p.User = Users().Name(p.UID)
	d := &Detail{Process: p}
	var err error
	if d.Exe, err = os.Readlink(base + "/exe"); err != nil {
		d.Errors = append(d.Errors, "exe: "+errText(err))
	}
	if d.Cwd, err = os.Readlink(base + "/cwd"); err != nil {
		d.Errors = append(d.Errors, "cwd: "+errText(err))
	}
	d.Root, _ = os.Readlink(base + "/root")
	if env, err := os.ReadFile(base + "/environ"); err == nil {
		for _, kv := range bytes.Split(env, []byte{0}) {
			if len(kv) > 0 {
				d.Environ = append(d.Environ, string(kv))
			}
		}
		sort.Strings(d.Environ)
	} else {
		d.Errors = append(d.Errors, "environ: "+errText(err))
	}
	if fds, err := os.ReadDir(base + "/fd"); err == nil {
		d.FDCount = len(fds)
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
			if err == nil && strings.HasPrefix(link, "socket:[") {
				if ino, err := strconv.ParseUint(link[8:len(link)-1], 10, 64); err == nil {
					d.Sockets = append(d.Sockets, ino)
				}
			}
		}
	} else {
		d.FDCount = -1
		d.Errors = append(d.Errors, "fd: "+errText(err))
	}
	if cg, err := os.ReadFile(base + "/cgroup"); err == nil {
		d.Cgroup = strings.TrimSpace(string(cg))
		d.Container = ContainerID(d.Cgroup)
	}
	if pp, ok := readProcess(p.PPID, BootTime()); ok {
		d.Parent = pp.Comm
	}
	// Children: scan stat of all processes (cheap enough on demand).
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		cpid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if b, err := os.ReadFile("/proc/" + e.Name() + "/stat"); err == nil {
			r := bytes.LastIndexByte(b, ')')
			if r > 0 {
				fs := strings.Fields(string(b[r+1:]))
				if len(fs) > 1 && atoi(fs[1]) == pid {
					d.Children = append(d.Children, cpid)
				}
			}
		}
	}
	return d, nil
}

// ContainerID extracts a 64-hex container id from cgroup content
// (docker, containerd, podman, systemd scopes).
func ContainerID(cgroup string) string {
	for _, line := range strings.Split(cgroup, "\n") {
		for _, part := range strings.FieldsFunc(line, func(r rune) bool {
			return r == '/' || r == '-' || r == ':' || r == '.'
		}) {
			if len(part) == 64 && isHex(part) {
				return part
			}
		}
	}
	return ""
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func errText(err error) string {
	if os.IsPermission(err) {
		return "permission denied (run as root for full info)"
	}
	return err.Error()
}

// InodeCache maps socket inode -> pid. Sockets rarely change owner, so the
// expensive walk over every /proc/[pid]/fd only runs when an unknown inode
// shows up, and stops as soon as all wanted inodes are found.
type InodeCache struct {
	m map[uint64]int
}

func NewInodeCache() *InodeCache { return &InodeCache{m: map[uint64]int{}} }

// Resolve returns inode -> pid for the wanted inodes (0 inodes are ignored).
func (c *InodeCache) Resolve(want []uint64) map[uint64]int {
	missing := map[uint64]bool{}
	for _, ino := range want {
		if ino == 0 {
			continue
		}
		// pid 0 means "owner not readable": cached too, otherwise every
		// tick would rescan everything as non-root.
		pid, ok := c.m[ino]
		if !ok || (pid != 0 && !pidAlive(pid)) {
			missing[ino] = true
		}
	}
	if len(missing) > 0 {
		c.scan(missing)
	}
	// keep the cache bounded to sockets that still exist
	out := make(map[uint64]int, len(want))
	for _, ino := range want {
		if pid, ok := c.m[ino]; ok {
			out[ino] = pid
		}
	}
	c.m = out
	return out
}

func pidAlive(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

func (c *InodeCache) scan(missing map[uint64]bool) {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			ino, err := strconv.ParseUint(link[8:len(link)-1], 10, 64)
			if err != nil {
				continue
			}
			if _, seen := c.m[ino]; !seen || missing[ino] {
				c.m[ino] = pid
			}
			if missing[ino] {
				delete(missing, ino)
				if len(missing) == 0 {
					return
				}
			}
		}
	}
	// whatever is still missing belongs to processes we cannot read
	for ino := range missing {
		c.m[ino] = 0
	}
}

// BootTime from /proc/stat btime.
func BootTime() time.Time {
	b, _ := os.ReadFile("/proc/stat")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "btime ") {
			return time.Unix(int64(atou(strings.TrimSpace(line[6:]))), 0)
		}
	}
	return time.Now()
}

// NumCPU counts cpuN lines in /proc/stat.
func NumCPU() int {
	b, _ := os.ReadFile("/proc/stat")
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			n++
		}
	}
	return max(1, n)
}

func totalCPUTicks() uint64 {
	b, _ := os.ReadFile("/proc/stat")
	line, _, _ := strings.Cut(string(b), "\n")
	var t uint64
	for _, f := range strings.Fields(line)[1:] {
		t += atou(f)
	}
	return t
}

// Tree orders processes as a parent/child tree with ASCII prefixes.
func Tree(ps []Process, less func(a, b *Process) bool) []Process {
	byPID := make(map[int]bool, len(ps))
	for _, p := range ps {
		byPID[p.PID] = true
	}
	kids := map[int][]Process{}
	var roots []Process
	for _, p := range ps {
		if p.PPID == 0 || !byPID[p.PPID] || p.PPID == p.PID {
			roots = append(roots, p)
		} else {
			kids[p.PPID] = append(kids[p.PPID], p)
		}
	}
	srt := func(l []Process) {
		sort.SliceStable(l, func(i, j int) bool { return less(&l[i], &l[j]) })
	}
	srt(roots)
	out := make([]Process, 0, len(ps))
	var walk func(p Process, depth int, prefix string, last bool)
	walk = func(p Process, depth int, prefix string, last bool) {
		p.Depth = depth
		if depth > 0 {
			if last {
				p.Prefix = prefix + "└─ "
			} else {
				p.Prefix = prefix + "├─ "
			}
		}
		out = append(out, p)
		c := kids[p.PID]
		srt(c)
		np := prefix
		if depth > 0 {
			if last {
				np += "   "
			} else {
				np += "│  "
			}
		}
		for i, k := range c {
			walk(k, depth+1, np, i == len(c)-1)
		}
	}
	for i, r := range roots {
		walk(r, 0, "", i == len(roots)-1)
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atou(s string) uint64 {
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}
