// Command proctab is a lightweight htop-style monitor for processes, ports,
// docker containers and system info.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/human-stuff/proctab/internal/ui"
)

// Set at build time: -ldflags "-X main.version=v0.1.0 -X main.commit=... -X main.date=..."
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// appName is the one place to change the program name.
const appName = "proctab"

func main() {
	delay := flag.Float64("d", 1.5, "refresh delay in seconds")
	start := flag.String("t", "processes", "start tab: processes, ports, docker, system")
	showVer := flag.Bool("v", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s %s - lightweight terminal system monitor\n\nusage: %s [flags]\n\n", appName, version, appName)
		flag.PrintDefaults()
	}
	flag.Parse()
	if version == "dev" {
		// `go install ...@vX.Y.Z` doesn't pass -ldflags, but records the module version.
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
	}
	if *showVer {
		fmt.Printf("%s %s (%s/%s)", appName, version, runtime.GOOS, runtime.GOARCH)
		if commit != "" {
			fmt.Printf(" commit %s built %s", commit, date)
		}
		fmt.Println()
		return
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintf(os.Stderr, "%s currently supports Linux only (/proc)\n", appName)
		os.Exit(1)
	}
	tabs := map[string]int{"processes": 0, "procs": 0, "ports": 1, "docker": 2, "system": 3, "sys": 3}
	t, ok := tabs[strings.ToLower(*start)]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown tab %q\n", *start)
		os.Exit(2)
	}
	d := time.Duration(*delay * float64(time.Second))
	if d < 200*time.Millisecond {
		d = 200 * time.Millisecond
	}
	app := ui.New(ui.Options{Name: appName, Version: version, Interval: d, StartTab: t})
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
