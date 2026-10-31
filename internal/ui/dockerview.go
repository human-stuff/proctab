package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/human-stuff/proctab/internal/docker"
)

type dockerView struct {
	a           *App
	table       *tview.Table
	runningOnly bool
	filter      string
	rows        []docker.Container
}

func newDockerView(a *App) *dockerView {
	v := &dockerView{a: a}
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
	return v
}

func (v *dockerView) primitive() tview.Primitive { return v.table }
func (v *dockerView) setFilter(s string)         { v.filter = s }
func (v *dockerView) getFilter() string          { return v.filter }

func (v *dockerView) keys() []barItem {
	mode := "Running"
	if v.runningOnly {
		mode = "All"
	}
	return []barItem{{"F1", "Help"}, {"F3", "Search"}, {"F4", "Filter"}, {"F5", mode},
		{"F10", "Quit"}, {"Enter", "Inspect"}}
}

func ctext(c docker.Container) string {
	return strings.ToLower(c.ID + " " + c.Name() + " " + c.Image + " " + c.State + " " + c.PortString())
}

func (v *dockerView) render() {
	t := v.table
	selID := ""
	if r, _ := t.GetSelection(); r > 0 && r-1 < len(v.rows) {
		selID = v.rows[r-1].ID
	}
	t.Clear()
	if v.a.d.dockerErr != "" {
		v.rows = nil
		t.SetCell(0, 0, tview.NewTableCell(" Docker: "+v.a.d.dockerErr).SetTextColor(colBad).SetSelectable(false))
		return
	}
	hdr := []string{"CONTAINER ID", "NAME", "IMAGE", "STATE", "CPU%", "MEM", "MEM LIMIT", "PORTS", "STATUS"}
	for c, h := range hdr {
		cell := tview.NewTableCell(h).SetStyle(colHeader).SetTransparency(false).SetSelectable(false)
		if c == len(hdr)-1 {
			cell.SetExpansion(1)
		}
		if h == "CPU%" || h == "MEM" || h == "MEM LIMIT" {
			cell.SetAlign(tview.AlignRight)
		}
		t.SetCell(0, c, cell)
	}
	f := strings.ToLower(v.filter)
	v.rows = v.rows[:0]
	for _, c := range v.a.d.containers {
		if v.runningOnly && c.State != "running" {
			continue
		}
		if f != "" && !strings.Contains(ctext(c), f) {
			continue
		}
		if s, ok := v.a.dstats[c.ID]; ok && c.State == "running" {
			c.CPU, c.MemUsed, c.MemLimit = s.CPU, s.MemUsed, s.MemLimit
		}
		v.rows = append(v.rows, c)
	}
	selRow := 1
	for i, c := range v.rows {
		r := i + 1
		if c.ID == selID {
			selRow = r
		}
		stCol := colBad
		switch c.State {
		case "running":
			stCol = colOK
		case "paused", "restarting", "created":
			stCol = colWarn
		}
		cpu, mem, lim := "-", "-", "-"
		if c.State == "running" {
			if _, ok := v.a.dstats[c.ID]; ok {
				cpu = fmt.Sprintf("%.1f", c.CPU)
				mem = bytesH(c.MemUsed)
				lim = bytesH(c.MemLimit)
			} else {
				cpu, mem, lim = "…", "…", "…"
			}
		}
		cells := []*tview.TableCell{
			tview.NewTableCell(c.ID[:min(12, len(c.ID))]).SetTextColor(colDim),
			tview.NewTableCell(esc(c.Name())).SetTextColor(tcell.ColorWhite).SetAttributes(tcell.AttrBold),
			tview.NewTableCell(esc(truncate(c.Image, 30))).SetTextColor(colLabel),
			tview.NewTableCell(c.State).SetTextColor(stCol).SetAttributes(tcell.AttrBold),
			numColor(cpu, hiColor(c.CPU, 50, 90)),
			num(mem),
			num(lim).SetTextColor(colDim),
			tview.NewTableCell(c.PortString()).SetTextColor(colOK),
			tview.NewTableCell(c.Status).SetExpansion(1),
		}
		for col, cell := range cells {
			t.SetCell(r, col, cell)
		}
	}
	if len(v.rows) == 0 {
		t.SetCell(1, 0, tview.NewTableCell(" no containers").SetTextColor(colDim).SetSelectable(false))
		return
	}
	t.Select(min(selRow, len(v.rows)), 0)
}

func (v *dockerView) search(s string, next bool) {
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
		if strings.Contains(ctext(v.rows[idx]), s) {
			v.table.Select(idx+1, 0)
			return
		}
	}
}

func (v *dockerView) open(row int) {
	if row > 0 && row-1 < len(v.rows) {
		v.a.detail.openContainer(v.rows[row-1].ID)
	}
}

func (v *dockerView) handle(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyF5 || ev.Key() == tcell.KeyRune && ev.Rune() == 'a' {
		v.runningOnly = !v.runningOnly
		v.render()
		v.a.renderFooter()
		return nil
	}
	return ev
}
