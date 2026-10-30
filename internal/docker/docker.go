// Package docker is a tiny Docker Engine API client over the unix socket.
// It intentionally avoids the official SDK to keep the binary small.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Client struct {
	sock string
	http *http.Client
}

// New finds the docker socket ($DOCKER_HOST, system, rootless).
func New() *Client {
	sock := "/var/run/docker.sock"
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		sock = strings.TrimPrefix(h, "unix://")
	} else if _, err := os.Stat(sock); err != nil {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			if _, err := os.Stat(xdg + "/docker.sock"); err == nil {
				sock = xdg + "/docker.sock"
			}
		}
	}
	c := &Client{sock: sock}
	c.http = &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}
	return c
}

// Err converts dial errors into friendly text.
func Err(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission):
		return "permission denied on docker socket (add your user to the 'docker' group or run as root)"
	case errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED):
		return "docker daemon not running (no socket)"
	}
	return err.Error()
}

func (c *Client) get(path string, v any) error {
	resp, err := c.http.Get("http://docker" + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type Port struct {
	IP          string
	PrivatePort uint16
	PublicPort  uint16
	Type        string
}

type Container struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
	Ports  []Port

	// Filled from stats.
	CPU      float64
	MemUsed  uint64
	MemLimit uint64
}

func (c Container) Name() string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:min(12, len(c.ID))]
}

func (c Container) PortString() string {
	seen := map[string]bool{}
	var parts []string
	for _, p := range c.Ports {
		var s string
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		}
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func (c *Client) List() ([]Container, error) {
	var out []Container
	err := c.get("/containers/json?all=1", &out)
	return out, err
}

type stats struct {
	CPUStats    cpuStats `json:"cpu_stats"`
	PreCPUStats cpuStats `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

// FillStats queries stats for running containers concurrently.
func (c *Client) FillStats(cs []Container) {
	var wg sync.WaitGroup
	for i := range cs {
		if cs[i].State != "running" {
			continue
		}
		wg.Add(1)
		go func(ct *Container) {
			defer wg.Done()
			var s stats
			if c.get("/containers/"+ct.ID+"/stats?stream=false", &s) != nil {
				return
			}
			cpuD := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
			sysD := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
			n := float64(s.CPUStats.OnlineCPUs)
			if n == 0 {
				n = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
			}
			if sysD > 0 && cpuD > 0 {
				ct.CPU = cpuD / sysD * n * 100
			}
			used := s.MemoryStats.Usage
			inactive := s.MemoryStats.Stats["inactive_file"]
			if inactive == 0 {
				inactive = s.MemoryStats.Stats["total_inactive_file"]
			}
			if inactive < used {
				used -= inactive
			}
			ct.MemUsed, ct.MemLimit = used, s.MemoryStats.Limit
		}(&cs[i])
	}
	wg.Wait()
}

// Inspect is the subset of /containers/{id}/json we display.
type Inspect struct {
	ID      string `json:"Id"`
	Name    string
	Created string
	Path    string
	Args    []string
	State   struct {
		Status     string
		Running    bool
		Pid        int
		ExitCode   int
		StartedAt  string
		FinishedAt string
	}
	RestartCount int
	Config       struct {
		Image      string
		Cmd        []string
		Entrypoint []string
		WorkingDir string
		User       string
		Env        []string
		Hostname   string
	}
	HostConfig struct {
		RestartPolicy struct{ Name string }
		NetworkMode   string
	}
	Mounts []struct {
		Type        string
		Source      string
		Destination string
		RW          bool
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIp, HostPort string }
		Networks map[string]struct {
			IPAddress string
			Gateway   string
		}
	}
}

func (c *Client) Inspect(id string) (*Inspect, error) {
	var in Inspect
	err := c.get("/containers/"+url.PathEscape(id)+"/json", &in)
	return &in, err
}

// Top returns host processes inside the container.
type Top struct {
	Titles    []string
	Processes [][]string
}

func (c *Client) Top(id string) (*Top, error) {
	var t Top
	err := c.get("/containers/"+url.PathEscape(id)+"/top", &t)
	return &t, err
}
