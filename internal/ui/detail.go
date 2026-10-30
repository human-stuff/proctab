package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/human-stuff/proctab/internal/docker"
	"github.com/human-stuff/proctab/internal/netstat"
	"github.com/human-stuff/proctab/internal/proc"
)

type entryKind int

const (
	kindProcess entryKind = iota
	kindSocket
	kindContainer
)

type entry struct {
	kind entryKind
	pid  int
	cid  string
	sock portRow

	// container data fetched asynchronously
	inspect  *docker.Inspect
	top      *docker.Top
	err      string
	fetching bool
}

type jump struct {
	label string
	pid   int
	cid   string
}

type detailView struct {
	a       *App
	flex    *tview.Flex
	text    *tview.TextView
	list    *tview.Table
	stack   []*entry
	jumps   []jump
	showEnv bool
}

func newDetailView(a *App) *detailView {
	v := &detailView{a: a}
	v.text = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	v.text.SetBorder(true).SetTitleAlign(tview.AlignLeft)
	v.list = tview.NewTable().SetSelectable(true, false).SetSelectedStyle(colSelect)
	v.list.SetBorder(true).SetTitle(" Related (Enter to open) ").SetTitleAlign(tview.AlignLeft)
	v.list.SetSelectedFunc(func(r, _ int) { v.follow(r) })
	v.list.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick {
			if r, _ := v.list.CellAt(ev.Position()); r >= 0 {
				v.follow(r)
				return action, nil
			}
		}
		return action, ev
	})
	v.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.text, 0, 3, true).
		AddItem(v.list, 0, 1, false)
	return v
}

func (v *detailView) keys() []barItem {
	env := "Env"
	if v.showEnv {
		env = "NoEnv"
	}
	return []barItem{{"F1", "Help"}, {"Esc", "Back"}, {"Tab", "Focus"}, {"Enter", "Open"},
		{"e", env}, {"F9", "Kill"}, {"F10", "Quit"}}
}

func (v *detailView) focus() tview.Primitive {
	if v.list.HasFocus() {
		return v.list
	}
	return v.text
}

func (v *detailView) push(e *entry) {
	v.stack = append(v.stack, e)
	v.a.pages.SwitchToPage("detail")
	v.text.ScrollToBeginning()
	v.a.app.SetFocus(v.text)
	if len(v.jumps) > 0 {
		v.list.Select(0, 0)
	}
	v.refresh()
	v.a.renderFooter()
}

func (v *detailView) openProcess(pid int)      { v.push(&entry{kind: kindProcess, pid: pid}) }
func (v *detailView) openSocket(r portRow)     { v.push(&entry{kind: kindSocket, sock: r}) }
func (v *detailView) openContainer(cid string) { v.push(&entry{kind: kindContainer, cid: cid}) }

func (v *detailView) back() {
	if len(v.stack) > 0 {
		v.stack = v.stack[:len(v.stack)-1]
	}
	if len(v.stack) == 0 {
		v.a.switchTab(tab(v.a.cur.Load()))
		v.a.renderFooter()
		return
	}
	v.refresh()
	v.a.app.SetFocus(v.text)
	v.a.renderFooter()
}

func (v *detailView) follow(r int) {
	if r < 0 || r >= len(v.jumps) {
		return
	}
	j := v.jumps[r]
	switch {
	case j.cid != "":
		v.openContainer(j.cid)
	case j.pid > 0:
		v.openProcess(j.pid)
	}
}

func (v *detailView) handle(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyLeft:
		v.back()
		return nil
	case tcell.KeyTab:
		if v.list.HasFocus() {
			v.a.app.SetFocus(v.text)
		} else {
			v.a.app.SetFocus(v.list)
		}
		return nil
	case tcell.KeyF9:
		v.kill()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			v.back()
			return nil
		case 'e':
			v.showEnv = !v.showEnv
			v.refresh()
			v.a.renderFooter()
			return nil
		case 'k':
			v.kill()
			return nil
		}
	}
	return ev
}

func (v *detailView) kill() {
	if len(v.stack) == 0 {
		return
	}
	e := v.stack[len(v.stack)-1]
	switch {
	case e.kind == kindProcess:
		v.a.killDialog(e.pid)
	case e.kind == kindContainer && e.inspect != nil && e.inspect.State.Pid > 0:
		v.a.killDialog(e.inspect.State.Pid)
	}
}

func (v *detailView) refresh() {
	if len(v.stack) == 0 {
		return
	}
	e := v.stack[len(v.stack)-1]
	var b strings.Builder
	v.jumps = v.jumps[:0]
	switch e.kind {
	case kindProcess:
		v.renderProcess(&b, e)
	case kindSocket:
		v.renderSocket(&b, e)
	case kindContainer:
		v.renderContainer(&b, e)
	}
	row, col := v.text.GetScrollOffset()
	v.text.SetText(b.String())
	v.text.ScrollTo(row, col)

	sel, _ := v.list.GetSelection()
	v.list.Clear()
	for i, j := range v.jumps {
		v.list.SetCell(i, 0, tview.NewTableCell(" "+j.label).SetExpansion(1))
	}
	if len(v.jumps) == 0 {
		v.list.SetCell(0, 0, tview.NewTableCell(" (nothing related)").SetTextColor(colDim).SetSelectable(false))
	} else {
		v.list.Select(min(sel, len(v.jumps)-1), 0)
	}
}

func dkv(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, " [teal]%-13s[-] %s\n", k, v)
}

func dsec(b *strings.Builder, t string) {
	fmt.Fprintf(b, "\n [black:green] %s [-:-]\n", t)
}

var stateNames = map[byte]string{
	'R': "running", 'S': "sleeping", 'D': "disk sleep", 'Z': "zombie", 'T': "stopped",
	't': "tracing stop", 'I': "idle", 'X': "dead",
}

func (v *detailView) renderProcess(b *strings.Builder, e *entry) {
	d, err := proc.ReadDetail(e.pid)
	v.text.SetTitle(fmt.Sprintf(" Process %d ", e.pid))
	if err != nil {
		fmt.Fprintf(b, "\n [red]Process %d has exited.[-]\n\n Press Esc to go back.", e.pid)
		return
	}
	// CPU% comes from the sampler (needs two samples).
	if p := v.a.d.byPID[e.pid]; p != nil {
		d.CPU, d.Mem = p.CPU, p.Mem
	}
	v.text.SetTitle(fmt.Sprintf(" Process %d — %s ", d.PID, esc(d.Comm)))
	parent := strconv.Itoa(d.PPID)
	if d.Parent != "" {
		parent += " (" + esc(d.Parent) + ")"
	}
	st := string(d.State)
	if n, ok := stateNames[d.State]; ok {
		st += " (" + n + ")"
	}
	dsec(b, "Process")
	dkv(b, "PID", fmt.Sprintf("[white::b]%d[-::-]", d.PID))
	dkv(b, "Name", "[white::b]"+esc(d.Comm)+"[-::-]")
	dkv(b, "Parent", parent)
	dkv(b, "User", fmt.Sprintf("%s (uid %d)", esc(d.User), d.UID))
	dkv(b, "State", st)
	dkv(b, "Started", fmt.Sprintf("%s  (running for %s)", d.Start.Format("2006-01-02 15:04:05"), dur(time.Since(d.Start))))
	dkv(b, "Threads", fmt.Sprintf("%d     Priority %s   Nice %d", d.Threads, prio(d.Priority), d.Nice))
	dkv(b, "CPU / MEM", fmt.Sprintf("%.1f%% / %.1f%%    CPU time %s", d.CPU, d.Mem, cpuTime(d.TimeTick)))
	dkv(b, "Memory", fmt.Sprintf("RES %s   VIRT %s   SHR %s", bytesH(d.ResKB*1024), bytesH(d.VirtKB*1024), bytesH(d.ShrKB*1024)))
	if d.FDCount >= 0 {
		dkv(b, "Open files", strconv.Itoa(d.FDCount))
	}

	dsec(b, "Command")
	cmd := d.Cmdline
	if cmd == "" {
		cmd = "[" + d.Comm + "]  (kernel thread)"
	}
	fmt.Fprintf(b, " [white::b]%s[-::-]\n\n", esc(cmd))
	dkv(b, "Executable", orNA(d.Exe))
	dkv(b, "Working dir", orNA(d.Cwd))
	if d.Root != "" && d.Root != "/" {
		dkv(b, "Root", esc(d.Root))
	}

	if d.Container != "" {
		dsec(b, "Container")
		name := d.Container[:12]
		if n, ok := v.a.d.cidName[d.Container]; ok {
			name = n
			v.jumps = append(v.jumps, jump{label: "[blue]container[-]  " + esc(n), cid: d.Container})
		}
		dkv(b, "Container", "[blue::b]"+esc(name)+"[-::-]  "+d.Container[:12])
	}

	// Sockets owned by this process.
	inodes := map[uint64]bool{}
	for _, i := range d.Sockets {
		inodes[i] = true
	}
	var listen, other []netstat.Socket
	for _, s := range v.a.d.socks {
		if !inodes[s.Inode] {
			continue
		}
		if s.Listening() {
			listen = append(listen, s)
		} else {
			other = append(other, s)
		}
	}
	if len(listen)+len(other) > 0 || len(d.Sockets) > 0 {
		dsec(b, fmt.Sprintf("Network  (%d listening, %d other)", len(listen), len(other)))
		sort.Slice(listen, func(i, j int) bool { return listen[i].LocalPort < listen[j].LocalPort })
		for _, s := range listen {
			svc := netstat.ServiceForPort(s.LocalPort)
			if svc != "" {
				svc = "  [yellow]" + svc + "[-]"
			}
			fmt.Fprintf(b, " [green::b]LISTEN[-::-]  %-5s %s:[green::b]%d[-::-]%s\n", s.Proto, s.LocalAddr, s.LocalPort, svc)
		}
		for i, s := range other {
			if i >= 30 {
				fmt.Fprintf(b, " [gray]… %d more[-]\n", len(other)-30)
				break
			}
			fmt.Fprintf(b, " [teal]%-11s[-] %-5s %s:%d -> %s:%d\n", s.State, s.Proto, s.LocalAddr, s.LocalPort, s.RemoteAddr, s.RemotePort)
		}
		if len(listen)+len(other) == 0 {
			fmt.Fprintf(b, " [gray]%d sockets (unix or other namespace)[-]\n", len(d.Sockets))
		}
	}

	if d.Cgroup != "" {
		dsec(b, "Cgroup")
		fmt.Fprintf(b, " [gray]%s[-]\n", esc(d.Cgroup))
	}

	dsec(b, fmt.Sprintf("Environment  (%d vars, press e to %s)", len(d.Environ), map[bool]string{true: "hide", false: "show"}[v.showEnv]))
	if v.showEnv {
		for _, kv := range d.Environ {
			k, val, _ := strings.Cut(kv, "=")
			fmt.Fprintf(b, " [teal]%s[-]=%s\n", esc(k), esc(val))
		}
	}
	if len(d.Errors) > 0 {
		dsec(b, "Not readable")
		for _, er := range d.Errors {
			fmt.Fprintf(b, " [yellow]%s[-]\n", esc(er))
		}
	}

	// Related: parent + children.
	if pp := v.a.d.byPID[d.PPID]; pp != nil || d.Parent != "" {
		v.jumps = append(v.jumps, jump{label: fmt.Sprintf("[teal]parent[-]     %6d  %s", d.PPID, esc(d.Parent)), pid: d.PPID})
	}
	sort.Ints(d.Children)
	for _, c := range d.Children {
		name := ""
		if p := v.a.d.byPID[c]; p != nil {
			name = p.Cmdline
			if name == "" {
				name = p.Comm
			}
		}
		v.jumps = append(v.jumps, jump{label: fmt.Sprintf("[green]child[-]      %6d  %s", c, esc(name)), pid: c})
	}
}

func orNA(s string) string {
	if s == "" {
		return "[gray]n/a[-]"
	}
	return esc(s)
}

func (v *detailView) renderSocket(b *strings.Builder, e *entry) {
	r := e.sock
	v.text.SetTitle(fmt.Sprintf(" Socket %s %d ", r.Proto, r.Port))
	dsec(b, "Socket")
	dkv(b, "Protocol", r.Proto)
	dkv(b, "Port", fmt.Sprintf("[green::b]%d[-::-]  %s", r.Port, r.Service))
	dkv(b, "Address", strings.Join(r.Addrs, ", "))
	dkv(b, "Owner uid", fmt.Sprintf("%d (%s)", r.UID, esc(r.User)))
	dkv(b, "Inode", strconv.FormatUint(r.Inode, 10))
	if r.Container != "" {
		dkv(b, "Container", "[blue::b]"+esc(r.Container)+"[-::-]")
		for id, n := range v.a.d.cidName {
			if n == r.Container {
				v.jumps = append(v.jumps, jump{label: "[blue]container[-]  " + esc(n), cid: id})
			}
		}
	}
	fmt.Fprintf(b, "\n [yellow]The owning process is not visible to this user.[-]\n")
	fmt.Fprintf(b, " Re-run with [white::b]sudo[-::-] to see PID, command, cwd and executable.\n")
	if r.Inode == 0 {
		fmt.Fprintf(b, " [gray](inode 0: socket is orphaned / in TIME_WAIT)[-]\n")
	}
}

func (v *detailView) fetchContainer(e *entry) {
	if e.fetching {
		return
	}
	e.fetching = true
	go func() {
		in, err := v.a.dk.Inspect(e.cid)
		var top *docker.Top
		var terr error
		if err == nil && in.State.Running {
			top, terr = v.a.dk.Top(e.cid)
		}
		v.a.app.QueueUpdateDraw(func() {
			e.fetching = false
			e.err = ""
			if err != nil {
				e.err = docker.Err(err)
			} else {
				e.inspect, e.top = in, top
				if terr != nil {
					e.err = terr.Error()
				}
			}
			if len(v.stack) > 0 && v.stack[len(v.stack)-1] == e && v.a.inDetail() {
				v.refresh()
			}
		})
	}()
}

func (v *detailView) renderContainer(b *strings.Builder, e *entry) {
	v.fetchContainer(e)
	v.text.SetTitle(fmt.Sprintf(" Container %s ", e.cid[:min(12, len(e.cid))]))
	in := e.inspect
	if in == nil {
		if e.err != "" {
			fmt.Fprintf(b, "\n [red]%s[-]\n", esc(e.err))
		} else {
			b.WriteString("\n [gray]loading…[-]\n")
		}
		return
	}
	name := strings.TrimPrefix(in.Name, "/")
	v.text.SetTitle(fmt.Sprintf(" Container %s — %s ", in.ID[:12], esc(name)))
	stCol := "red"
	if in.State.Running {
		stCol = "green"
	}
	dsec(b, "Container")
	dkv(b, "Name", "[white::b]"+esc(name)+"[-::-]")
	dkv(b, "ID", in.ID)
	dkv(b, "Image", "[teal]"+esc(in.Config.Image)+"[-]")
	dkv(b, "State", fmt.Sprintf("[%s::b]%s[-::-]  exit code %d  restarts %d", stCol, in.State.Status, in.State.ExitCode, in.RestartCount))
	dkv(b, "Started", in.State.StartedAt)
	if !in.State.Running {
		dkv(b, "Finished", in.State.FinishedAt)
	}
	dkv(b, "Created", in.Created)
	if in.State.Pid > 0 {
		dkv(b, "Host PID", fmt.Sprintf("[white::b]%d[-::-]", in.State.Pid))
		v.jumps = append(v.jumps, jump{label: fmt.Sprintf("[green]init[-]       %6d  %s", in.State.Pid, esc(in.Path)), pid: in.State.Pid})
	}
	dkv(b, "Restart", in.HostConfig.RestartPolicy.Name)

	dsec(b, "Command")
	fmt.Fprintf(b, " [white::b]%s[-::-]\n\n", esc(strings.TrimSpace(in.Path+" "+strings.Join(in.Args, " "))))
	if len(in.Config.Entrypoint) > 0 {
		dkv(b, "Entrypoint", esc(strings.Join(in.Config.Entrypoint, " ")))
	}
	if len(in.Config.Cmd) > 0 {
		dkv(b, "Cmd", esc(strings.Join(in.Config.Cmd, " ")))
	}
	dkv(b, "Working dir", orNA(in.Config.WorkingDir))
	if in.Config.User != "" {
		dkv(b, "User", esc(in.Config.User))
	}

	dsec(b, "Network")
	dkv(b, "Mode", in.HostConfig.NetworkMode)
	for n, nw := range in.NetworkSettings.Networks {
		dkv(b, esc(truncate(n, 13)), fmt.Sprintf("ip %s  gw %s", nw.IPAddress, nw.Gateway))
	}
	var ports []string
	for p := range in.NetworkSettings.Ports {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	for _, p := range ports {
		binds := in.NetworkSettings.Ports[p]
		if len(binds) == 0 {
			fmt.Fprintf(b, " [teal]%-13s[-] %s [gray](not published)[-]\n", "Port", p)
		}
		for _, bd := range binds {
			fmt.Fprintf(b, " [teal]%-13s[-] [green::b]%s:%s[-::-] -> %s\n", "Port", bd.HostIp, bd.HostPort, p)
		}
	}

	if len(in.Mounts) > 0 {
		dsec(b, "Mounts")
		for _, m := range in.Mounts {
			rw := "rw"
			if !m.RW {
				rw = "ro"
			}
			fmt.Fprintf(b, " [teal]%-6s[-] %s -> [white]%s[-] (%s)\n", m.Type, esc(m.Source), esc(m.Destination), rw)
		}
	}

	if e.top != nil && len(e.top.Processes) > 0 {
		dsec(b, fmt.Sprintf("Processes  (%d, host PIDs — see list below)", len(e.top.Processes)))
		pidIdx, cmdIdx := -1, len(e.top.Titles)-1
		for i, t := range e.top.Titles {
			if t == "PID" {
				pidIdx = i
			}
		}
		fmt.Fprintf(b, " [teal]%s[-]\n", esc(strings.Join(e.top.Titles, "  ")))
		for _, p := range e.top.Processes {
			fmt.Fprintf(b, " %s\n", esc(strings.Join(p, "  ")))
			if pidIdx >= 0 && pidIdx < len(p) {
				if pid, err := strconv.Atoi(p[pidIdx]); err == nil && pid != in.State.Pid {
					cmd := ""
					if cmdIdx < len(p) {
						cmd = p[cmdIdx]
					}
					v.jumps = append(v.jumps, jump{label: fmt.Sprintf("[green]process[-]    %6d  %s", pid, esc(cmd)), pid: pid})
				}
			}
		}
	}
	if e.err != "" {
		fmt.Fprintf(b, "\n [yellow]%s[-]\n", esc(e.err))
	}

	dsec(b, fmt.Sprintf("Environment  (%d vars, press e to %s)", len(in.Config.Env), map[bool]string{true: "hide", false: "show"}[v.showEnv]))
	if v.showEnv {
		for _, kv := range in.Config.Env {
			k, val, _ := strings.Cut(kv, "=")
			fmt.Fprintf(b, " [teal]%s[-]=%s\n", esc(k), esc(val))
		}
	}
}
