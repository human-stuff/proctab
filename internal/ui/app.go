// Package ui is the htop-style terminal interface.
package ui

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/human-stuff/proctab/internal/docker"
	"github.com/human-stuff/proctab/internal/netstat"
	"github.com/human-stuff/proctab/internal/proc"
	"github.com/human-stuff/proctab/internal/sysinfo"
)

type tab int32

const (
	tabProcs tab = iota
	tabPorts
	tabDocker
	tabSystem
)

var tabNames = []string{"Processes", "Ports", "Docker", "System"}

// portRow is a socket enriched with its owner process.
type portRow struct {
	netstat.Listener
	Sock      *netstat.Socket // set in "all sockets" mode
	Service   string
	Comm      string
	Cmd       string
	User      string
	Container string
}

// data is one full collection pass; replaced atomically on the UI goroutine.
type data struct {
	snap       sysinfo.Snapshot
	procs      []proc.Process
	byPID      map[int]*proc.Process
	threads    int
	socks      []netstat.Socket
	listen     []portRow
	all        []portRow
	containers []docker.Container
	dockerErr  string
	cidName    map[string]string
	at         time.Time
}

type Options struct {
	Name     string
	Version  string
	Interval time.Duration
	StartTab int
}

type App struct {
	opt    Options
	app    *tview.Application
	static sysinfo.Static
	sys    *sysinfo.Sampler
	ps     *proc.Sampler
	dk     *docker.Client
	inodes *proc.InodeCache
	d      data
	dstats map[string]docker.Container

	cur     atomic.Int32
	refresh chan struct{}
	root    *tview.Flex
	header  *tview.TextView
	tabbar  *clickBar
	fkeys   *clickBar
	footer  *tview.Pages
	input   *tview.InputField
	pages   *tview.Pages
	width   int
	status  string
	statusT time.Time

	procs  *procView
	ports  *portsView
	docker *dockerView
	system *tview.TextView
	detail *detailView
	help   *tview.TextView
}

// view is implemented by each tab.
type view interface {
	primitive() tview.Primitive
	render()
	keys() []barItem
	setFilter(string)
	getFilter() string
	search(string, bool)
	handle(ev *tcell.EventKey) *tcell.EventKey
}

func New(opt Options) *App {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.BorderColor = tcell.ColorTeal
	tview.Styles.TitleColor = tcell.ColorTeal

	a := &App{
		opt:     opt,
		app:     tview.NewApplication(),
		static:  sysinfo.ReadStatic(),
		sys:     sysinfo.NewSampler(),
		ps:      proc.NewSampler(),
		dk:      docker.New(),
		inodes:  proc.NewInodeCache(),
		dstats:  map[string]docker.Container{},
		refresh: make(chan struct{}, 1),
	}
	a.header = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.tabbar = newClickBar(true)
	for i, n := range tabNames {
		a.tabbar.items = append(a.tabbar.items, barItem{fmt.Sprint(i + 1), n})
	}
	a.tabbar.onClick = func(i int) { a.switchTab(tab(i)) }
	a.tabbar.right = fmt.Sprintf("%s %s ", opt.Name, opt.Version)
	a.fkeys = newClickBar(false)
	a.fkeys.onClick = a.clickFKey
	a.input = tview.NewInputField().
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorTeal)
	a.footer = tview.NewPages().
		AddPage("keys", a.fkeys, true, true).
		AddPage("input", a.input, true, false)

	a.procs = newProcView(a)
	a.ports = newPortsView(a)
	a.docker = newDockerView(a)
	a.system = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	a.system.SetBorderPadding(0, 0, 1, 1)
	a.detail = newDetailView(a)
	a.help = tview.NewTextView().SetDynamicColors(true).SetText(helpText(opt.Name, opt.Version))
	a.help.SetBorder(true).SetTitle(" Help ")

	a.pages = tview.NewPages()
	for i, v := range a.views() {
		a.pages.AddPage(tabNames[i], v, true, i == 0)
	}
	a.pages.AddPage("detail", a.detail.flex, true, false)
	a.pages.AddPage("help", a.help, true, false)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, a.headerHeight(), 0, false).
		AddItem(a.tabbar, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.footer, 1, 0, false)

	a.app.SetRoot(a.root, true).EnableMouse(true)
	a.app.SetInputCapture(a.globalKeys)
	a.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		if w, _ := s.Size(); w != a.width {
			a.width = w
			a.renderHeader()
			a.ports.layout()
		}
		return false
	})
	start := tab(opt.StartTab)
	if start < tabProcs || start > tabSystem {
		start = tabProcs
	}
	a.switchTab(start)
	return a
}

func (a *App) views() []tview.Primitive {
	return []tview.Primitive{a.procs.primitive(), a.ports.primitive(), a.docker.primitive(), a.system}
}

func (a *App) view() view {
	switch tab(a.cur.Load()) {
	case tabPorts:
		return a.ports
	case tabDocker:
		return a.docker
	case tabProcs:
		return a.procs
	}
	return nil
}

// Run starts the collector and the event loop.
func (a *App) Run() error {
	a.d = a.collect() // first sample primes CPU counters
	go a.loop()
	go a.dockerStatsLoop()
	return a.app.Run()
}

func (a *App) loop() {
	time.Sleep(300 * time.Millisecond)
	t := time.NewTicker(a.opt.Interval)
	defer t.Stop()
	for {
		d := a.collect()
		a.app.QueueUpdateDraw(func() {
			a.d = d
			a.renderAll()
		})
		select {
		case <-t.C:
		case <-a.refresh:
		}
	}
}

func (a *App) kick() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

func (a *App) dockerStatsLoop() {
	for {
		if tab(a.cur.Load()) == tabDocker {
			cs, err := a.dk.List()
			if err == nil {
				a.dk.FillStats(cs)
				a.app.QueueUpdateDraw(func() {
					for _, c := range cs {
						a.dstats[c.ID] = c
					}
					a.docker.render()
				})
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *App) collect() data {
	var d data
	d.at = time.Now()
	d.snap = a.sys.Sample(tab(a.cur.Load()) == tabSystem)
	d.procs = a.ps.Scan(d.snap.Mem.TotalKB)
	d.byPID = make(map[int]*proc.Process, len(d.procs))
	for i := range d.procs {
		d.byPID[d.procs[i].PID] = &d.procs[i]
		d.threads += d.procs[i].Threads
	}
	cur := tab(a.cur.Load())
	d.socks = netstat.Read("/proc")
	// Mapping sockets to PIDs walks every fd of every process: the most
	// expensive step, so only do it when the Ports tab needs it.
	inodes := map[uint64]int{}
	if cur == tabPorts {
		want := make([]uint64, 0, len(d.socks))
		for _, s := range d.socks {
			if s.Listening() || a.ports.all {
				want = append(want, s.Inode)
			}
		}
		inodes = a.inodes.Resolve(want)
	}
	for i := range d.socks {
		d.socks[i].PID = inodes[d.socks[i].Inode]
	}

	var cs []docker.Container
	if cur != tabProcs {
		var err error
		cs, err = a.dk.List()
		d.dockerErr = docker.Err(err)
	} else {
		cs, d.dockerErr = a.d.containers, a.d.dockerErr
	}
	d.containers = cs
	d.cidName = map[string]string{}
	pubPort := map[uint16]string{}
	for _, c := range cs {
		d.cidName[c.ID] = c.Name()
		for _, p := range c.Ports {
			if p.PublicPort != 0 {
				pubPort[p.PublicPort] = c.Name()
			}
		}
	}

	users := proc.Users()
	cgCache := map[int]string{}
	enrich := func(r *portRow) {
		r.Service = netstat.ServiceForPort(r.Port)
		r.User = users.Name(r.UID)
		if p := d.byPID[r.PID]; p != nil {
			r.Comm, r.Cmd, r.User = p.Comm, p.Cmdline, p.User
			if r.Service == "" {
				for _, s := range netstat.KnownServices {
					for _, n := range s.Procs {
						if n == p.Comm {
							r.Service = s.Name
						}
					}
				}
			}
		}
		if n, ok := pubPort[r.Port]; ok && (r.Comm == "docker-proxy" || r.Comm == "" || r.Comm == "dockerd") {
			r.Container = n
		} else if r.PID > 0 {
			id, ok := cgCache[r.PID]
			if !ok {
				if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", r.PID)); err == nil {
					id = proc.ContainerID(string(b))
				}
				cgCache[r.PID] = id
			}
			if id != "" {
				if n, ok := d.cidName[id]; ok {
					r.Container = n
				} else {
					r.Container = id[:12]
				}
			}
		}
	}
	for _, l := range netstat.Listeners(d.socks, inodes) {
		r := portRow{Listener: l}
		enrich(&r)
		d.listen = append(d.listen, r)
	}
	for i := range d.socks {
		s := &d.socks[i]
		r := portRow{Listener: netstat.Listener{
			Proto: strings.ToUpper(s.Proto), Port: s.LocalPort, PID: s.PID, UID: s.UID, Inode: s.Inode,
			Addrs: []string{s.LocalAddr.String()},
		}, Sock: s}
		enrich(&r)
		d.all = append(d.all, r)
	}
	return d
}

func (a *App) renderAll() {
	a.renderHeader()
	switch tab(a.cur.Load()) {
	case tabProcs:
		a.procs.render()
	case tabPorts:
		a.ports.render()
	case tabDocker:
		a.docker.render()
	case tabSystem:
		a.renderSystem()
	}
	if a.inDetail() {
		a.detail.refresh()
	}
	a.renderFooter()
}

func (a *App) inDetail() bool {
	name, _ := a.pages.GetFrontPage()
	return name == "detail"
}

func (a *App) switchTab(t tab) {
	a.cur.Store(int32(t))
	a.tabbar.active = int(t)
	a.detail.stack = nil
	a.pages.SwitchToPage(tabNames[t])
	a.app.SetFocus(a.views()[t])
	if a.d.at.IsZero() {
		return
	}
	a.renderAll()
	if t == tabSystem {
		a.kick() // disks are only sampled on the system tab
	}
}

func (a *App) setStatus(s string) {
	a.status, a.statusT = s, time.Now()
	a.renderFooter()
}

func (a *App) renderFooter() {
	switch {
	case a.inDetail():
		a.fkeys.items = a.detail.keys()
	case a.view() != nil:
		a.fkeys.items = a.view().keys()
	default:
		a.fkeys.items = []barItem{{"F1", "Help"}, {"Tab", "Next"}, {"F10", "Quit"}}
	}
	a.fkeys.right = ""
	if a.status != "" && time.Since(a.statusT) < 5*time.Second {
		a.fkeys.right = a.status
	} else if v := a.view(); v != nil && v.getFilter() != "" && !a.inDetail() {
		a.fkeys.right = "Filter: " + v.getFilter()
	} else if tab(a.cur.Load()) == tabPorts && a.ports.hidden > 0 && !isRoot() && !a.inDetail() {
		a.fkeys.right = fmt.Sprintf("%d sockets with hidden owner (run with sudo)", a.ports.hidden)
	}
}

func (a *App) clickFKey(i int) {
	if i >= len(a.fkeys.items) {
		return
	}
	k := a.fkeys.items[i].key
	var ev *tcell.EventKey
	if strings.HasPrefix(k, "F") && len(k) > 1 {
		var n int
		fmt.Sscanf(k[1:], "%d", &n)
		ev = tcell.NewEventKey(tcell.KeyF1+tcell.Key(n-1), 0, tcell.ModNone)
	} else if k == "Esc" {
		ev = tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	} else if k == "Enter" {
		ev = tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)
	} else if k == "Tab" {
		ev = tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	} else {
		ev = tcell.NewEventKey(tcell.KeyRune, []rune(k)[0], tcell.ModNone)
	}
	if ev = a.globalKeys(ev); ev != nil {
		if h := a.app.GetFocus().InputHandler(); h != nil {
			h(ev, func(p tview.Primitive) { a.app.SetFocus(p) })
		}
	}
}

// prompt opens the footer input (search / filter) htop-style.
func (a *App) prompt(label, initial string, onChange func(string), onDone func(text string, ok bool)) {
	prev := a.app.GetFocus()
	a.input.SetLabel(label).SetText(initial)
	a.input.SetChangedFunc(onChange)
	a.input.SetDoneFunc(func(k tcell.Key) {
		a.footer.SwitchToPage("keys")
		a.app.SetFocus(prev)
		onDone(a.input.GetText(), k == tcell.KeyEnter)
	})
	a.footer.SwitchToPage("input")
	a.app.SetFocus(a.input)
}

func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if a.app.GetFocus() == a.input {
		return ev
	}
	front, _ := a.pages.GetFrontPage()
	if front == "help" {
		a.pages.HidePage("help")
		a.app.SetFocus(a.currentPrimitive())
		return nil
	}
	if front == "modal" {
		return ev
	}
	if ev.Key() == tcell.KeyCtrlC {
		a.app.Stop()
		return nil
	}
	if ev.Key() == tcell.KeyCtrlL {
		a.app.Sync()
		return nil
	}
	if a.inDetail() {
		if e := a.detail.handle(ev); e == nil {
			return nil
		}
	}
	switch ev.Key() {
	case tcell.KeyF10:
		a.app.Stop()
		return nil
	case tcell.KeyF1:
		a.showHelp()
		return nil
	case tcell.KeyTab:
		a.switchTab((tab(a.cur.Load()) + 1) % 4)
		return nil
	case tcell.KeyBacktab:
		a.switchTab((tab(a.cur.Load()) + 3) % 4)
		return nil
	case tcell.KeyF3:
		a.startSearch()
		return nil
	case tcell.KeyF4:
		a.startFilter()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			a.app.Stop()
			return nil
		case '1', '2', '3', '4':
			a.switchTab(tab(ev.Rune() - '1'))
			return nil
		case '?', 'h':
			a.showHelp()
			return nil
		case '/':
			a.startSearch()
			return nil
		case '\\':
			a.startFilter()
			return nil
		}
	}
	if a.inDetail() {
		return ev
	}
	if v := a.view(); v != nil {
		return v.handle(ev)
	}
	return ev
}

func (a *App) currentPrimitive() tview.Primitive {
	if a.inDetail() {
		return a.detail.focus()
	}
	return a.views()[a.cur.Load()]
}

func (a *App) showHelp() {
	a.pages.ShowPage("help")
	a.app.SetFocus(a.help)
}

func (a *App) startSearch() {
	v := a.view()
	if v == nil || a.inDetail() {
		return
	}
	a.prompt("Search: ", "", func(s string) { v.search(s, false) }, func(s string, ok bool) {})
}

func (a *App) startFilter() {
	v := a.view()
	if v == nil || a.inDetail() {
		return
	}
	old := v.getFilter()
	a.prompt("Filter: ", old, func(s string) { v.setFilter(s); v.render() }, func(s string, ok bool) {
		if !ok {
			v.setFilter("")
		}
		v.render()
		a.renderFooter()
	})
}

// confirm shows a modal with buttons, red like htop's kill menu.
func (a *App) confirm(text string, buttons []string, done func(label string)) {
	prev := a.app.GetFocus()
	m := tview.NewModal().SetText(text).AddButtons(buttons).
		SetDoneFunc(func(_ int, label string) {
			a.pages.RemovePage("modal")
			a.app.SetFocus(prev)
			done(label)
		})
	m.SetBackgroundColor(tcell.ColorDarkRed).SetTextColor(tcell.ColorWhite)
	m.SetButtonBackgroundColor(tcell.ColorBlack).SetButtonTextColor(tcell.ColorWhite)
	m.SetBorderColor(tcell.ColorWhite)
	a.pages.AddPage("modal", m, true, true)
	a.app.SetFocus(m)
}

func (a *App) headerHeight() int {
	n := a.static.Cores
	if n == 0 {
		n = proc.NumCPU()
	}
	cols := headerCols(n)
	return (n+cols-1)/cols + 3
}

func headerCols(n int) int {
	switch {
	case n <= 1:
		return 1
	case n <= 32:
		return 2
	default:
		return 4
	}
}

func (a *App) renderHeader() {
	w := a.width
	if w < 40 {
		w = 80
	}
	s := a.d.snap
	cpus := s.CPUs
	if len(cpus) > 1 {
		cpus = cpus[1:]
	}
	n := len(cpus)
	cols := headerCols(n)
	cw := (w - 1 - (cols - 1)) / cols
	rows := (n + cols - 1) / cols
	var b strings.Builder
	digits := len(fmt.Sprint(n - 1))
	for r := 0; r < rows; r++ {
		b.WriteString(" ")
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= n {
				break
			}
			if c > 0 {
				b.WriteString(" ")
			}
			cp := cpus[i]
			label := fmt.Sprintf("%*d", digits+1, i)
			b.WriteString(meter(label, cw, []segment{
				{cp.Low, "blue"}, {cp.User, "green"}, {cp.Sys, "red"},
			}, fmt.Sprintf("%.1f%%", cp.Total)))
		}
		b.WriteString("\n")
	}
	half := (w - 2) / 2
	m := s.Mem
	pct := func(v, t uint64) float64 {
		if t == 0 {
			return 0
		}
		return float64(v) / float64(t) * 100
	}
	memText := fmt.Sprintf("%s/%s", kb(m.UsedKB), kb(m.TotalKB))
	memLine := meter(fmt.Sprintf("%*s", digits+1, "Mem"), half, []segment{
		{pct(m.UsedKB, m.TotalKB), "green"}, {pct(m.BuffersKB, m.TotalKB), "blue"}, {pct(m.CachedKB, m.TotalKB), "yellow"},
	}, memText)
	swpLine := meter(fmt.Sprintf("%*s", digits+1, "Swp"), half, []segment{
		{pct(m.SwapUsedKB, m.SwapTotalKB), "red"},
	}, fmt.Sprintf("%s/%s", kb(m.SwapUsedKB), kb(m.SwapTotalKB)))
	running := 0
	for _, p := range a.d.procs {
		if p.State == 'R' {
			running++
		}
	}
	tasks := fmt.Sprintf("[teal]Tasks: [white::b]%d[teal::-], [green::b]%d[teal::-] thr; [green::b]%d[teal::-] running",
		len(a.d.procs), a.d.threads, running)
	load := fmt.Sprintf("[teal]Load average: [white::b]%.2f [teal::b]%.2f [teal::-]%.2f", s.Load[0], s.Load[1], s.Load[2])
	up := fmt.Sprintf("[teal]Uptime: [white::b]%s", dur(s.Uptime))
	fmt.Fprintf(&b, " %s  %s\n", memLine, tasks)
	fmt.Fprintf(&b, " %s  %s\n", swpLine, load)
	fmt.Fprintf(&b, " %s  %s", strings.Repeat(" ", half), up)
	a.header.SetText(b.String())
}

func helpText(name, version string) string {
	return fmt.Sprintf(`
 [white::b]%s %s[-::-] - lightweight terminal system monitor (processes, ports, docker, system)

 [teal]Global[-]
   [white::b]1 2 3 4[-::-]   switch tab: Processes, Ports, Docker, System   ([white::b]Tab[-::-]/[white::b]Shift-Tab[-::-] cycle)
   [white::b]F1 ? h[-::-]    this help                [white::b]F3 /[-::-]   search          [white::b]F4 \[-::-]  filter
   [white::b]Enter[-::-]     drill down into selected  [white::b]Esc[-::-]    back            [white::b]F10 q[-::-] quit
   [white::b]Ctrl-L[-::-]    redraw screen            mouse: click tabs, F-keys, column headers; double-click opens

 [teal]Processes[-]
   [white::b]F5 t[-::-]  tree view     [white::b]F6 < >[-::-]  sort column    [white::b]P M T N[-::-]  sort CPU / MEM / TIME / PID
   [white::b]I[-::-]     invert sort   [white::b]K[-::-]       show/hide kernel threads   [white::b]u[-::-] only my processes
   [white::b]F7 ][-::-]  nice -        [white::b]F8 [[-::-]    nice +         [white::b]F9 k[-::-]  send signal (kill)

 [teal]Ports[-]
   [white::b]F5 a[-::-]  toggle listening / all sockets   [white::b]s[-::-] focus service grid (Enter filters by service)
   Service colours: [green]● listening[-]   [yellow]◐ idle: process running, no port[-]   [red]○ not running[-]

 [teal]Docker[-]
   [white::b]F5 a[-::-]  toggle all / running containers   Enter shows inspect + processes (open host PID)

 [teal]Detail view[-]
   [white::b]Tab[-::-] switch focus text/list   [white::b]Enter[-::-] open selected PID   [white::b]e[-::-] environment   [white::b]F9[-::-] kill   [white::b]Esc[-::-] back

 Run as root to see owners of every socket, cwd/exe/environ of other users.

 [gray]Press any key to close.[-]`, name, version)
}
