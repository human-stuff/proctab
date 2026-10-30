package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// kb formats kibibytes htop-style (no decimals below 100M).
func kb(v uint64) string {
	switch {
	case v < 100*1024:
		return fmt.Sprintf("%d", v)
	case v < 10*1024*1024:
		return fmt.Sprintf("%dM", v/1024)
	case v < 100*1024*1024:
		return fmt.Sprintf("%.1fG", float64(v)/1024/1024)
	default:
		return fmt.Sprintf("%dG", v/1024/1024)
	}
}

// bytesH formats bytes with one decimal.
func bytesH(v uint64) string {
	const u = 1024.0
	f := float64(v)
	units := []string{"B", "K", "M", "G", "T", "P"}
	i := 0
	for f >= u && i < len(units)-1 {
		f /= u
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", v)
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func rate(v float64) string {
	return bytesH(uint64(v)) + "/s"
}

// cpuTime formats ticks as htop TIME+ (M:SS.cc or HhMM:SS).
func cpuTime(ticks uint64) string {
	cs := ticks // 100 ticks per second == centiseconds
	s := cs / 100
	if s >= 3600 {
		return fmt.Sprintf("%dh%02d:%02d", s/3600, (s/60)%60, s%60)
	}
	return fmt.Sprintf("%d:%02d.%02d", s/60, s%60, cs%100)
}

func dur(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%d day%s, %02d:%02d:%02d", days, plural(days), h, m, s)
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func esc(s string) string { return tview.Escape(s) }

func pad(s string, n int) string {
	if len([]rune(s)) >= n {
		return string([]rune(s)[:n])
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
