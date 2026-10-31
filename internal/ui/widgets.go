package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Colours, close to htop's default scheme.
var (
	colLabel   = tcell.ColorTeal
	colBracket = tcell.ColorWhite
	colOK      = tcell.ColorGreen
	colBad     = tcell.ColorRed
	colWarn    = tcell.ColorYellow
	colLow     = tcell.ColorBlue
	colDim     = tcell.ColorGray
	colHeader  = tcell.StyleDefault.Background(tcell.ColorGreen).Foreground(tcell.ColorBlack)
	colSelect  = tcell.StyleDefault.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack)
	styleDef   = tcell.StyleDefault
)

// segment is one coloured piece of a meter.
type segment struct {
	pct   float64
	color string // tview colour tag name
}

// meter renders an htop bar: "LBL[|||||||      text]" with width w.
func meter(label string, w int, segs []segment, text string) string {
	inner := w - len(label) - 2
	if inner < 4 {
		inner = 4
	}
	// Fill a cell buffer with bar chars, then overlay the right-aligned
	// text on top (htop draws the value over the bar).
	chars := []rune(strings.Repeat(" ", inner))
	colors := make([]string, inner)
	used := 0
	for _, s := range segs {
		n := int(math.Round(s.pct / 100 * float64(inner)))
		for i := 0; i < n && used < inner; i++ {
			chars[used], colors[used] = '|', s.color
			used++
		}
	}
	if tr := []rune(text); len(tr) <= inner {
		off := inner - len(tr)
		for i, r := range tr {
			chars[off+i], colors[off+i] = r, "gray"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[teal::b]%s[white::b][", label)
	last := ""
	for i, r := range chars {
		c := colors[i]
		if c == "" {
			c = "default"
		}
		if c != last {
			fmt.Fprintf(&b, "[%s::-]", c)
			last = c
		}
		b.WriteRune(r)
	}
	b.WriteString("[white::b]][-::-]")
	return b.String()
}

// clickBar is a one-line bar of clickable items (tab bar and F-key bar).
type clickBar struct {
	*tview.Box
	items   []barItem
	active  int // highlighted item for tab bars, -1 for none
	tabs    bool
	onClick func(i int)
	right   string // right-aligned text
	spans   [][2]int
}

type barItem struct {
	key, label string
}

func newClickBar(tabs bool) *clickBar {
	return &clickBar{Box: tview.NewBox(), tabs: tabs, active: -1}
}

func (c *clickBar) Draw(screen tcell.Screen) {
	c.Box.DrawForSubclass(screen, c)
	x, y, w, _ := c.GetInnerRect()
	keyStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	lblStyle := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorTeal)
	if c.tabs {
		for i := 0; i < w; i++ {
			screen.SetContent(x+i, y, ' ', nil, tcell.StyleDefault.Background(tcell.ColorBlue))
		}
	}
	c.spans = c.spans[:0]
	cx := x
	put := func(s string, st tcell.Style) {
		for _, r := range s {
			if cx >= x+w {
				return
			}
			screen.SetContent(cx, y, r, nil, st)
			cx++
		}
	}
	for i, it := range c.items {
		start := cx
		if c.tabs {
			st := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlue)
			kst := st.Foreground(tcell.ColorYellow).Bold(true)
			if i == c.active {
				st = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorTeal).Bold(true)
				kst = st
			}
			put(" "+it.key, kst)
			put(" "+it.label+" ", st)
			put(" ", tcell.StyleDefault.Background(tcell.ColorBlue))
		} else {
			put(it.key, keyStyle)
			put(pad(it.label, max(6, len([]rune(it.label))+1)), lblStyle)
		}
		c.spans = append(c.spans, [2]int{start, cx})
	}
	if c.right != "" {
		st := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlue)
		if !c.tabs {
			st = tcell.StyleDefault.Foreground(tcell.ColorTeal)
		}
		rx := x + w - len([]rune(c.right)) - 1
		if rx > cx {
			cx = rx
			put(c.right, st)
		}
	}
}

func (c *clickBar) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return c.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if action != tview.MouseLeftClick || !c.InRect(event.Position()) {
			return false, nil
		}
		mx, _ := event.Position()
		for i, sp := range c.spans {
			if mx >= sp[0] && mx < sp[1] && c.onClick != nil {
				c.onClick(i)
				return true, nil
			}
		}
		return true, nil
	})
}
