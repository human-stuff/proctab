package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"proctab/internal/netstat"
)

const svcCellWidth = 20

type svcState int

const (
	svcDown svcState = iota
	svcRunning
	svcListening
)

type svcStatus struct {
	svc   netstat.Service
	state svcState
	ports []uint16
}

type portsView struct {
	a         *App
	flex      *tview.Flex
	svcTable  *tview.Table
	table     *tview.Table
	all       bool
	filter    string
	svcFilter string
	rows      []portRow
	svcs      []svcStatus
	svcCols   int
	hidden    int
}

func newPortsView(a *App) *portsView {
	v := &portsView{a: a, svcCols: 4}
	v.svcTable = tview.NewTable().SetSelectable(false, false).SetSelectedStyle(colSelect)
	v.svcTable.SetBorder(true).SetTitle(" Services ").SetTitleAlign(tview.AlignLeft)
	v.svcTable.SetSelectedFunc(func(r, c int) { v.toggleSvc(r*v.svcCols + c) })
	v.svcTable.SetDoneFunc(func(tcell.Key) { v.focusTable() })
	v.svcTable.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick {
			r, c := v.svcTable.CellAt(ev.Position())
			if r >= 0 && c >= 0 {
				v.toggleSvc(r*v.svcCols + c)
			}
			return action, nil
		}
		return action, ev
	})

	v.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false).SetSelectedStyle(colSelect)
	v.table.SetSelectedFunc(func(row, _ int) { v.open(row) })
	v.table.SetMouseCapture(func(action tview.MouseAction, ev *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftDoubleClick {
			if row, _ := v.table.CellAt(ev.Position()); row > 0 {
				v.open(row)
				return action, nil
			}
		}
		return action, ev
	})
	v.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.svcTable, 4, 0, false).
		AddItem(v.table, 0, 1, true)
	return v
}

func (v *portsView) primitive() tview.Primitive { return v.flex }
func (v *portsView) setFilter(s string)         { v.filter = s }
func (v *portsView) getFilter() string          { return v.filter }

func (v *portsView) keys() []barItem {
	mode := "All"
	if v.all {
		mode = "Listen"
	}
	return []barItem{{"F1", "Help"}, {"F3", "Search"}, {"F4", "Filter"}, {"F5", mode},
		{"F9", "Kill"}, {"F10", "Quit"}, {"s", "Svcs"}, {"Enter", "Info"}}
}

// layout resizes the service grid for the current width.
func (v *portsView) layout() {
	w := v.a.width - 2
	v.svcCols = max(1, w/svcCellWidth)
	rows := (len(netstat.KnownServices) + v.svcCols - 1) / v.svcCols
	v.flex.ResizeItem(v.svcTable, rows+2, 0)
}

func (v *portsView) services() []svcStatus {
	d := v.a.d
	running := map[string]bool{}
	for _, p := range d.procs {
		running[p.Comm] = true
	}
	var out []svcStatus
	for _, s := range netstat.KnownServices {
		st := svcStatus{svc: s}
		seen := map[uint16]bool{}
		for _, l := range d.listen {
			if svcMatches(s, l) && !seen[l.Port] {
				seen[l.Port] = true
				st.ports = append(st.ports, l.Port)
			}
		}
		sort.Slice(st.ports, func(i, j int) bool { return st.ports[i] < st.ports[j] })
		switch {
		case len(st.ports) > 0:
			st.state = svcListening
		default:
			for _, n := range s.Procs {
				if running[n] {
					st.state = svcRunning
				}
			}
		}
		out = append(out, st)
	}
	return out
}

func svcMatches(s netstat.Service, l portRow) bool {
	for _, p := range s.Ports {
		if p == l.Port {
			return true
		}
	}
	for _, n := range s.Procs {
		if n == l.Comm {
			return true
		}
	}
	return false
}

func (v *portsView) toggleSvc(i int) {
	if i < 0 || i >= len(v.svcs) {
		return
	}
	n := v.svcs[i].svc.Name
	if v.svcFilter == n {
		v.svcFilter = ""
		v.a.setStatus("service filter cleared")
	} else {
		v.svcFilter = n
		v.a.setStatus("showing service: " + n + " (click again to clear)")
	}
	v.render()
}

func (v *portsView) focusTable() {
	v.svcTable.SetSelectable(false, false)
	v.a.app.SetFocus(v.table)
}

func (v *portsView) render() {
	v.svcs = v.services()
	st := v.svcTable
	st.Clear()
	for i, s := range v.svcs {
		var txt string
		var col tcell.Color
		switch s.state {
		case svcListening:
			ps := make([]string, 0, len(s.ports))
			for _, p := range s.ports {
				ps = append(ps, strconv.Itoa(int(p)))
			}
			txt, col = "● "+s.svc.Name+" :"+strings.Join(ps, ","), colOK
		case svcRunning:
			txt, col = "◐ "+s.svc.Name+" (no port)", colWarn
		default:
			txt, col = "○ "+s.svc.Name, colBad
		}
		cell := tview.NewTableCell(pad(truncate(txt, svcCellWidth-1), svcCellWidth-1)).SetTextColor(col)
		if s.svc.Name == v.svcFilter {
			cell.SetAttributes(tcell.AttrReverse | tcell.AttrBold)
		} else if s.state == svcListening {
			cell.SetAttributes(tcell.AttrBold)
		}
		st.SetCell(i/v.svcCols, i%v.svcCols, cell)
	}
	listening := 0
	for _, s := range v.svcs {
		if s.state == svcListening {
			listening++
		}
	}
	st.SetTitle(fmt.Sprintf(" Services: [green]%d up[teal] / [red]%d down[teal] ", listening, len(v.svcs)-listening))

	var svc *netstat.Service
	for i := range netstat.KnownServices {
		if netstat.KnownServices[i].Name == v.svcFilter {
			svc = &netstat.KnownServices[i]
		}
	}
	src := v.a.d.listen
	if v.all {
		src = v.a.d.all
	}
	f := strings.ToLower(v.filter)
	var rows []portRow
	for _, r := range src {
		if svc != nil && !svcMatches(*svc, r) {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(rowText(r)), f) {
			continue
		}
		rows = append(rows, r)
	}
	if v.all {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Sock.State != rows[j].Sock.State {
				return rows[i].Sock.State == "LISTEN" || rows[j].Sock.State != "LISTEN" && rows[i].Sock.State < rows[j].Sock.State
			}
			return rows[i].Port < rows[j].Port
		})
	}

	selKey := ""
	if r, _ := v.table.GetSelection(); r > 0 && r-1 < len(v.rows) {
		selKey = rowKey(v.rows[r-1])
	}
	v.rows = rows
	t := v.table
	t.Clear()
	var hdr []string
	if v.all {
		hdr = []string{"PROTO", "LOCAL", "PORT", "REMOTE", "STATE", "PID", "USER", "PROCESS", "CONTAINER", "COMMAND"}
	} else {
		hdr = []string{"PROTO", "PORT", "ADDRESS", "SERVICE", "PID", "USER", "PROCESS", "CONTAINER", "COMMAND"}
	}
	for c, h := range hdr {
		cell := tview.NewTableCell(h).SetStyle(colHeader).SetSelectable(false)
		if c == len(hdr)-1 {
			cell.SetExpansion(1)
		}
		t.SetCell(0, c, cell)
	}
	hidden := 0
	selRow := 1
	for i, r := range rows {
		row := i + 1
		if rowKey(r) == selKey {
			selRow = row
		}
		pid := "-"
		pidCol := colBad
		if r.PID > 0 {
			pid, pidCol = strconv.Itoa(r.PID), tcell.ColorDefault
		} else {
			hidden++
		}
		comm := r.Comm
		if comm == "" {
			comm = "?"
		}
		protoCol := colLabel
		if strings.HasPrefix(r.Proto, "UDP") {
			protoCol = tcell.ColorFuchsia
		}
		var cells []*tview.TableCell
		if v.all {
			s := r.Sock
			stCol := tcell.ColorDefault
			switch s.State {
			case "LISTEN":
				stCol = colOK
			case "ESTABLISHED":
				stCol = colLabel
			case "TIME_WAIT", "CLOSE_WAIT", "FIN_WAIT1", "FIN_WAIT2":
				stCol = colWarn
			}
			remote := s.RemoteAddr.String() + ":" + strconv.Itoa(int(s.RemotePort))
			if s.RemotePort == 0 {
				remote = "*"
			}
			cells = []*tview.TableCell{
				tview.NewTableCell(strings.ToLower(s.Proto)).SetTextColor(protoCol),
				tview.NewTableCell(esc(truncate(s.LocalAddr.String(), 28))),
				num(strconv.Itoa(int(s.LocalPort))).SetAttributes(tcell.AttrBold),
				tview.NewTableCell(esc(truncate(remote, 30))),
				tview.NewTableCell(s.State).SetTextColor(stCol),
				numColor(pid, pidCol),
				tview.NewTableCell(truncate(r.User, 10)),
				tview.NewTableCell(esc(comm)).SetTextColor(tcell.ColorWhite),
				tview.NewTableCell(r.Container).SetTextColor(colLow),
				tview.NewTableCell(esc(r.Cmd)).SetExpansion(1).SetTextColor(colDim),
			}
		} else {
			cells = []*tview.TableCell{
				tview.NewTableCell(r.Proto).SetTextColor(protoCol),
				num(strconv.Itoa(int(r.Port))).SetTextColor(colOK).SetAttributes(tcell.AttrBold),
				tview.NewTableCell(esc(truncate(strings.Join(r.Addrs, ","), 32))),
				tview.NewTableCell(r.Service).SetTextColor(colWarn),
				numColor(pid, pidCol),
				tview.NewTableCell(truncate(r.User, 10)),
				tview.NewTableCell(esc(comm)).SetTextColor(tcell.ColorWhite).SetAttributes(tcell.AttrBold),
				tview.NewTableCell(r.Container).SetTextColor(colLow),
				tview.NewTableCell(esc(r.Cmd)).SetExpansion(1).SetTextColor(colDim),
			}
		}
		for c, cell := range cells {
			t.SetCell(row, c, cell)
		}
	}
	if len(rows) > 0 {
		t.Select(min(selRow, len(rows)), 0)
	}
	v.hidden = hidden
}

func rowKey(r portRow) string {
	return fmt.Sprintf("%s/%d/%d/%d", r.Proto, r.Port, r.PID, r.Inode)
}

func rowText(r portRow) string {
	return fmt.Sprintf("%s %d %s %s %d %s %s %s %s", r.Proto, r.Port, strings.Join(r.Addrs, " "),
		r.Service, r.PID, r.User, r.Comm, r.Container, r.Cmd)
}

func (v *portsView) search(s string, next bool) {
	s = strings.ToLower(s)
	if s == "" || len(v.rows) == 0 {
		return
	}
	start, _ := v.table.GetSelection()
	if !next {
		start--
	}
	for i := 0; i < len(v.rows); i++ {
		idx := ((start+i)%len(v.rows) + len(v.rows)) % len(v.rows)
		if strings.Contains(strings.ToLower(rowText(v.rows[idx])), s) {
			v.table.Select(idx+1, 0)
			return
		}
	}
}

func (v *portsView) open(row int) {
	if row <= 0 || row-1 >= len(v.rows) {
		return
	}
	r := v.rows[row-1]
	if r.PID > 0 {
		v.a.detail.openProcess(r.PID)
	} else {
		v.a.detail.openSocket(r)
	}
}

func (v *portsView) handle(ev *tcell.EventKey) *tcell.EventKey {
	if v.a.app.GetFocus() == v.svcTable {
		if ev.Key() == tcell.KeyEscape || (ev.Key() == tcell.KeyRune && ev.Rune() == 's') {
			v.focusTable()
			return nil
		}
		return ev
	}
	switch ev.Key() {
	case tcell.KeyF5:
		v.all = !v.all
	case tcell.KeyF9:
		if r, _ := v.table.GetSelection(); r > 0 && r-1 < len(v.rows) {
			v.a.killDialog(v.rows[r-1].PID)
		}
		return nil
	case tcell.KeyEscape:
		if v.svcFilter == "" {
			return ev
		}
		v.svcFilter = ""
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'a':
			v.all = !v.all
		case 's':
			v.svcTable.SetSelectable(true, true)
			v.svcTable.Select(0, 0)
			v.a.app.SetFocus(v.svcTable)
			return nil
		case 'k':
			if r, _ := v.table.GetSelection(); r > 0 && r-1 < len(v.rows) {
				v.a.killDialog(v.rows[r-1].PID)
			}
			return nil
		default:
			return ev
		}
	default:
		return ev
	}
	v.table.Select(1, 0)
	v.render()
	v.a.renderFooter()
	return nil
}
