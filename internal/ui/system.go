package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func isRoot() bool { return os.Geteuid() == 0 }

func section(b *strings.Builder, title string) {
	fmt.Fprintf(b, "\n[black:green] %s [-:-]\n", title)
}

func kv(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, " [teal]%-14s[-] %s\n", k, v)
}

func (a *App) renderSystem() {
	var b strings.Builder
	st, s, d := a.static, a.d.snap, a.d
	w := max(40, a.width-4)

	section(&b, "System")
	kv(&b, "Hostname", "[white::b]"+esc(st.Hostname)+"[-::-]")
	kv(&b, "OS", esc(st.OS))
	kv(&b, "Kernel", esc(st.Kernel)+"  ("+st.Arch+")")
	kv(&b, "Uptime", dur(s.Uptime))
	kv(&b, "Booted", time.Now().Add(-s.Uptime).Format("2006-01-02 15:04:05"))
	user := os.Getenv("USER")
	if isRoot() {
		user += " [green](root: full visibility)[-]"
	} else {
		user += " [yellow](non-root: other users' sockets/cwd hidden)[-]"
	}
	kv(&b, "Running as", user)

	section(&b, "CPU")
	kv(&b, "Model", esc(st.CPUModel))
	kv(&b, "Cores", fmt.Sprint(st.Cores))
	kv(&b, "Load avg", fmt.Sprintf("%.2f %.2f %.2f  (1/5/15 min)", s.Load[0], s.Load[1], s.Load[2]))
	if len(s.CPUs) > 0 {
		c := s.CPUs[0]
		b.WriteString(" " + meter(fmt.Sprintf("%-15s", "Total"), min(w, 90), []segment{
			{c.Low, "blue"}, {c.User, "green"}, {c.Sys, "red"},
		}, fmt.Sprintf("%.1f%% (usr %.1f sys %.1f)", c.Total, c.User, c.Sys)) + "\n")
	}

	section(&b, "Memory")
	m := s.Mem
	pct := func(v, t uint64) float64 {
		if t == 0 {
			return 0
		}
		return float64(v) / float64(t) * 100
	}
	b.WriteString(" " + meter(fmt.Sprintf("%-15s", "RAM"), min(w, 90), []segment{
		{pct(m.UsedKB, m.TotalKB), "green"}, {pct(m.BuffersKB, m.TotalKB), "blue"}, {pct(m.CachedKB, m.TotalKB), "yellow"},
	}, fmt.Sprintf("%s used / %s", bytesH(m.UsedKB*1024), bytesH(m.TotalKB*1024))) + "\n")
	b.WriteString(" " + meter(fmt.Sprintf("%-15s", "Swap"), min(w, 90), []segment{
		{pct(m.SwapUsedKB, m.SwapTotalKB), "red"},
	}, fmt.Sprintf("%s / %s", bytesH(m.SwapUsedKB*1024), bytesH(m.SwapTotalKB*1024))) + "\n")
	kv(&b, "Available", bytesH(m.AvailKB*1024))
	kv(&b, "Buffers/Cache", bytesH(m.BuffersKB*1024)+" / "+bytesH(m.CachedKB*1024))

	section(&b, "Disks")
	fmt.Fprintf(&b, " [teal]%-24s %-8s %-28s %9s %9s %9s  %s[-]\n", "MOUNT", "FS", "DEVICE", "SIZE", "USED", "AVAIL", "USE%")
	for _, dk := range s.Disks {
		p := 0.0
		if dk.Used+dk.Avail > 0 {
			p = float64(dk.Used) / float64(dk.Used+dk.Avail) * 100
		}
		col := "green"
		if p > 90 {
			col = "red"
		} else if p > 75 {
			col = "yellow"
		}
		bar := meter("", 22, []segment{{p, col}}, fmt.Sprintf("%.0f%%", p))
		fmt.Fprintf(&b, " %-24s %-8s %-28s %9s %9s %9s  %s\n", esc(truncate(dk.Mount, 24)), dk.FSType,
			esc(truncate(dk.Device, 28)), bytesH(dk.Total), bytesH(dk.Used), bytesH(dk.Avail), bar)
	}

	section(&b, "Network")
	fmt.Fprintf(&b, " [teal]%-16s %-5s %12s %12s %10s %10s  %s[-]\n", "IFACE", "STATE", "RX/s", "TX/s", "RX total", "TX total", "ADDRESSES")
	for _, n := range s.Nets {
		state := "[red]down[-]"
		if n.Up {
			state = "[green]up  [-]"
		}
		fmt.Fprintf(&b, " %-16s %s  %12s %12s %10s %10s  %s\n", esc(truncate(n.Name, 16)), state,
			rate(n.RxRate), rate(n.TxRate), bytesH(n.RxBytes), bytesH(n.TxBytes), esc(strings.Join(n.Addrs, " ")))
	}

	section(&b, "Summary")
	users := map[string]bool{}
	zombies := 0
	for _, p := range d.procs {
		users[p.User] = true
		if p.State == 'Z' {
			zombies++
		}
	}
	kv(&b, "Processes", fmt.Sprintf("%d  (%d threads, %d zombies, %d users)", len(d.procs), d.threads, zombies, len(users)))
	est := 0
	for _, sk := range d.socks {
		if sk.State == "ESTABLISHED" {
			est++
		}
	}
	kv(&b, "Sockets", fmt.Sprintf("%d listening, %d established, %d total", len(d.listen), est, len(d.socks)))
	if d.dockerErr != "" {
		kv(&b, "Docker", "[red]"+esc(d.dockerErr)+"[-]")
	} else {
		run := 0
		for _, c := range d.containers {
			if c.State == "running" {
				run++
			}
		}
		kv(&b, "Docker", fmt.Sprintf("[green]%d running[-] / %d containers", run, len(d.containers)))
	}

	row, col := a.system.GetScrollOffset()
	a.system.SetText(b.String())
	a.system.ScrollTo(row, col)
}
