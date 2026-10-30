package proc

import (
	"os"
	"testing"
	"time"
)

func TestContainerID(t *testing.T) {
	id := "4c011ebee3029ebd2512b084d28e596a5b03600bb7863aa87e369b080cd16091"
	for _, cg := range []string{
		"0::/system.slice/docker-" + id + ".scope",
		"0::/docker/" + id,
		"12:memory:/docker/" + id,
		"0::/kubepods/besteffort/pod1/cri-containerd-" + id + ".scope",
	} {
		if got := ContainerID(cg); got != id {
			t.Errorf("ContainerID(%q) = %q", cg, got)
		}
	}
	if got := ContainerID("0::/user.slice/user-1000.slice/session-2.scope"); got != "" {
		t.Errorf("host cgroup detected as container: %q", got)
	}
}

func TestReadSelf(t *testing.T) {
	p, ok := readProcess(os.Getpid(), BootTime())
	if !ok {
		t.Fatal("cannot read own process")
	}
	if p.PID != os.Getpid() || p.PPID != os.Getppid() {
		t.Errorf("pid/ppid mismatch: %+v", p)
	}
	if time.Since(p.Start) > time.Hour || time.Since(p.Start) < 0 {
		t.Errorf("odd start time %v", p.Start)
	}
}

func TestTree(t *testing.T) {
	ps := []Process{{PID: 1}, {PID: 3, PPID: 1}, {PID: 2, PPID: 1}, {PID: 4, PPID: 2}}
	out := Tree(ps, func(a, b *Process) bool { return a.PID < b.PID })
	want := []int{1, 2, 4, 3}
	for i, p := range out {
		if p.PID != want[i] {
			t.Fatalf("order %v, want %v", out, want)
		}
	}
	if out[2].Prefix != "│  └─ " {
		t.Errorf("prefix %q", out[2].Prefix)
	}
}
