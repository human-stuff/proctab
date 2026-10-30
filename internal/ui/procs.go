package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"proctab/internal/proc"
)

type procCol struct {
	name  string
	right bool
	less  func(a, b *proc.Process) bool
}

var procCols = []procCol{
	{"PID", true, func(a, b *proc.Process) bool { return a.PID < b.PID }},
	{"USER", false, func(a, b *proc.Process) bool { return a.User < b.User }},
	{"PRI", true, func(a, b *proc.Process) bool { return a.Priority < b.Priority }},
	{"NI", true, func(a, b *proc.Process) bool { return a.Nice < b.Nice }},
	{"VIRT", true, func(a, b *proc.Process) bool { return a.VirtKB < b.VirtKB }},
	{"RES", true, func(a, b *proc.Process) bool { return a.ResKB < b.ResKB }},
	{"SHR", true, func(a, b *proc.Process) bool { return a.ShrKB < b.ShrKB }},
	{"S", false, func(a, b *proc.Process) bool { return a.State < b.State }},
	{"CPU%", true, func(a, b *proc.Process) bool { return a.CPU < b.CPU }},
	{"MEM%", true, func(a, b *proc.Process) bool { return a.ResKB < b.ResKB }},
	{"TIME+", true, func(a, b *proc.Process) bool { return a.TimeTick < b.TimeTick }},
	{"Command", false, func(a, b *proc.Process) bool { return a.Cmdline < b.Cmdline }},
}

const (
	colPID  = 0
	colCPU  = 8
	colMEM  = 9
	colTIME = 10
)

type procView struct {
	a          *App
	table      *tview.Table
	sortCol    int
	desc       bool
	tree       bool
	hideKernel bool
	onlyMine   bool
	filter     string
	rows       []proc.Process
}

func newProcView(a *App) *procView {
	v := &procView{a: a, sortCol: colCPU, desc: true, hideKernel: true}
	v.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).
		SetSelectedStyle(colSelect)
	v.table.SetSelectedFunc(func(row, _ int) { v.open(row) })
	v.table.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		x, y := ev.Position()
		row, col := v.table.CellAt(x, y)
		if action == tview.MouseLeftClick && row == 0 && col >= 0 {
			if v.sortCol == col {
				v.desc = !v.desc
			} else {
				v.sortCol, v.desc = col, col != colPID && col != 1 && col != 11
			}
			v.render()
			return action, nil
		}
		if action == tview.MouseLeftDoubleClick && row > 0 {
			v.open(row)
			return action, nil
		}
		return action, ev
	})
	return v
}

func (v *procView) primitive() tview.Primitive { return v.table }
func (v *procView) setFilter(s string)         { v.filter = s }
func (v *procView) getFilter() string          { return v.filter }

func (v *procView) keys() []barItem {
	tree := "Tree"
	if v.tree {
		tree = "List"
	}
	return []barItem{{"F1", "Help"}, {"F3", "Search"}, {"F4", "Filter"}, {"F5", tree},
		{"F6", "SortBy"}, {"F7", "Nice -"}, {"F8", "Nice +"}, {"F9", "Kill"}, {"F10", "Quit"}, {"Enter", "Info"}}
}

func (v *procView) less(a, b *proc.Process) bool {
	c := procCols[v.sortCol]
	if v.desc {
		if c.less(b, a) {
			return true
		}
		if c.less(a, b) {
			return false
		}
	} else {
		if c.less(a, b) {
			return true
		}
		if c.less(b, a) {
			return false
		}
	}
	return a.PID < b.PID
}

func (v *procView) match(p *proc.Process) bool {
	if v.hideKernel && p.Kernel {
		return false
	}
	if v.onlyMine && p.UID != os.Getuid() {
		return false
	}
	if v.filter == "" {
		return true
	}
	f := strings.ToLower(v.filter)
	return strings.Contains(strings.ToLower(p.Cmdline), f) ||
		strings.Contains(strings.ToLower(p.Comm), f) ||
		strings.Contains(strings.ToLower(p.User), f) ||
		strconv.Itoa(p.PID) == f
}

func (v *procView) selectedPID() int {
	r, _ := v.table.GetSelection()
	if r > 0 && r-1 < len(v.rows) {
		return v.rows[r-1].PID
	}
	return -1
}

func (v *procView) render() {
	sel := v.selectedPID()
	var list []proc.Process
	for i := range v.a.d.procs {
		if v.match(&v.a.d.procs[i]) {
			list = append(list, v.a.d.procs[i])
		}
	}
	if v.tree {
		list = proc.Tree(list, func(a, b *proc.Process) bool { return a.PID < b.PID })
	} else {
		sort.SliceStable(list, func(i, j int) bool { return v.less(&list[i], &list[j]) })
	}
	v.rows = list

	t := v.table
	t.Clear()
	for c, col := range procCols {
		name := col.name
		st := colHeader
		if c == v.sortCol && !v.tree {
			st = colSelect.Bold(true)
		}
		cell := tview.NewTableCell(name).SetStyle(st).SetSelectable(false)
		if col.right {
			cell.SetAlign(tview.AlignRight)
		}
		if c == len(procCols)-1 {
			cell.SetExpansion(1)
		}
		t.SetCell(0, c, cell)
	}
	me := os.Getuid()
	selRow := -1
	for i := range list {
		p := &list[i]
		r := i + 1
		if p.PID == sel {
			selRow = r
		}
		userCol := tcell.ColorDefault
		if p.UID != me && p.UID != 0 {
			userCol = colDim
		}
		state := string(p.State)
		stCol := tcell.ColorDefault
		switch p.State {
		case 'R':
			stCol = colOK
		case 'D':
			stCol = colBad
		case 'Z', 'T':
			stCol = colWarn
		}
		cmd := p.Cmdline
		cmdCol := tcell.ColorDefault
		if cmd == "" {
			cmd = "[" + p.Comm + "]"
			cmdCol = colDim
		}
		cells := []*tview.TableCell{
			num(strconv.Itoa(p.PID)),
			tview.NewTableCell(truncate(p.User, 9)).SetTextColor(userCol),
			num(prio(p.Priority)),
			numColor(strconv.Itoa(p.Nice), niceColor(p.Nice)),
			memCell(p.VirtKB),
			memCell(p.ResKB),
			memCell(p.ShrKB),
			tview.NewTableCell(state).SetTextColor(stCol),
			numColor(fmt.Sprintf("%.1f", p.CPU), hiColor(p.CPU, 50, 90)),
			numColor(fmt.Sprintf("%.1f", p.Mem), hiColor(p.Mem, 20, 50)),
			numColor(cpuTime(p.TimeTick), tcell.ColorDefault),
			tview.NewTableCell(p.Prefix + highlightCmd(esc(cmd), cmdCol)).SetExpansion(1),
		}
		for c, cell := range cells {
			t.SetCell(r, c, cell)
		}
	}
	if selRow < 0 {
		selRow = 1
		if r, _ := t.GetSelection(); r > 0 && r < t.GetRowCount() {
			selRow = r
		}
	}
	if len(list) > 0 {
		t.Select(min(selRow, len(list)), 0)
	}
}

// highlightCmd bolds the program basename like htop.
func highlightCmd(cmd string, base tcell.Color) string {
	if base == colDim {
		return "[gray]" + cmd
	}
	first, rest, _ := strings.Cut(cmd, " ")
	dir, file := filepath.Split(first)
	if rest != "" {
		rest = " " + rest
	}
	return "[teal]" + dir + "[white::b]" + file + "[-::-]" + rest
}

func prio(p int) string {
	if p <= -100 {
		return "RT"
	}
	return strconv.Itoa(p)
}

func niceColor(n int) tcell.Color {
	switch {
	case n < 0:
		return colBad
	case n > 0:
		return colLow
	}
	return tcell.ColorDefault
}

func hiColor(v, warn, bad float64) tcell.Color {
	switch {
	case v >= bad:
		return colBad
	case v >= warn:
		return colWarn
	}
	return tcell.ColorDefault
}

func num(s string) *tview.TableCell {
	return tview.NewTableCell(s).SetAlign(tview.AlignRight)
}

func numColor(s string, c tcell.Color) *tview.TableCell {
	return num(s).SetTextColor(c)
}

func memCell(v uint64) *tview.TableCell {
	s := kb(v)
	c := tcell.ColorDefault
	if strings.HasSuffix(s, "G") {
		c = colOK
	} else if strings.HasSuffix(s, "M") {
		c = colLabel
	}
	return numColor(s, c)
}

func (v *procView) search(s string, next bool) {
	if s == "" {
		return
	}
	s = strings.ToLower(s)
	start, _ := v.table.GetSelection()
	if !next {
		start--
	}
	for i := 0; i < len(v.rows); i++ {
		idx := (start + i) % len(v.rows)
		if idx < 0 {
			idx += len(v.rows)
		}
		p := v.rows[idx]
		if strings.Contains(strings.ToLower(p.Cmdline+" "+p.Comm), s) {
			v.table.Select(idx+1, 0)
			return
		}
	}
}

func (v *procView) open(row int) {
	if row > 0 && row-1 < len(v.rows) {
		v.a.detail.openProcess(v.rows[row-1].PID)
	}
}

func (v *procView) cycleSort(d int) {
	v.sortCol = (v.sortCol + d + len(procCols)) % len(procCols)
	v.render()
}

func (v *procView) setSort(col int) {
	if v.sortCol == col {
		v.desc = !v.desc
	} else {
		v.sortCol, v.desc = col, col != colPID
	}
	v.tree = false
	v.render()
}

func (v *procView) handle(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyF5:
		v.tree = !v.tree
	case tcell.KeyF6:
		v.cycleSort(1)
		v.a.setStatus("Sort: " + procCols[v.sortCol].name)
	case tcell.KeyF7:
		v.renice(-1)
	case tcell.KeyF8:
		v.renice(1)
	case tcell.KeyF9:
		v.a.killDialog(v.selectedPID())
	case tcell.KeyRune:
		switch ev.Rune() {
		case 't':
			v.tree = !v.tree
		case '>', '.':
			v.cycleSort(1)
		case '<', ',':
			v.cycleSort(-1)
		case 'P':
			v.setSort(colCPU)
		case 'M':
			v.setSort(colMEM)
		case 'T':
			v.setSort(colTIME)
		case 'N':
			v.setSort(colPID)
		case 'I':
			v.desc = !v.desc
		case 'K':
			v.hideKernel = !v.hideKernel
		case 'u':
			v.onlyMine = !v.onlyMine
		case 'k':
			v.a.killDialog(v.selectedPID())
		case ']':
			v.renice(-1)
		case '[':
			v.renice(1)
		case 'n':
			v.search(v.a.input.GetText(), true)
		default:
			return ev
		}
	default:
		return ev
	}
	v.render()
	v.a.renderFooter()
	return nil
}

func (v *procView) renice(delta int) {
	pid := v.selectedPID()
	p := v.a.d.byPID[pid]
	if p == nil {
		return
	}
	n := max(-20, min(19, p.Nice+delta))
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, n); err != nil {
		v.a.setStatus(fmt.Sprintf("renice %d: %v", pid, err))
		return
	}
	p.Nice = n
	v.a.setStatus(fmt.Sprintf("PID %d nice = %d", pid, n))
	v.a.kick()
}

var signals = []struct {
	name string
	sig  syscall.Signal
}{
	{"15 TERM", syscall.SIGTERM}, {"9 KILL", syscall.SIGKILL}, {"1 HUP", syscall.SIGHUP},
	{"2 INT", syscall.SIGINT}, {"19 STOP", syscall.SIGSTOP}, {"18 CONT", syscall.SIGCONT},
}

func (a *App) killDialog(pid int) {
	if pid <= 0 {
		return
	}
	name := ""
	if p := a.d.byPID[pid]; p != nil {
		name = p.Comm
	}
	var btns []string
	for _, s := range signals {
		btns = append(btns, s.name)
	}
	btns = append(btns, "Cancel")
	a.confirm(fmt.Sprintf("Send signal to PID %d (%s)?", pid, name), btns, func(label string) {
		for _, s := range signals {
			if s.name == label {
				if err := syscall.Kill(pid, s.sig); err != nil {
					a.setStatus(fmt.Sprintf("kill %d: %v", pid, err))
				} else {
					a.setStatus(fmt.Sprintf("sent SIG%s to %d", strings.Fields(label)[1], pid))
				}
				a.kick()
			}
		}
	})
}
