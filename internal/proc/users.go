package proc

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UserDB maps uid -> username from /etc/passwd (no cgo).
type UserDB map[int]string

var (
	userMu    sync.Mutex
	userCache UserDB
	userRead  time.Time
)

// Users returns a cached passwd map, refreshed every 30s.
func Users() UserDB {
	userMu.Lock()
	defer userMu.Unlock()
	if userCache != nil && time.Since(userRead) < 30*time.Second {
		return userCache
	}
	db := UserDB{}
	b, _ := os.ReadFile("/etc/passwd")
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 {
			if uid, err := strconv.Atoi(f[2]); err == nil {
				db[uid] = f[0]
			}
		}
	}
	userCache, userRead = db, time.Now()
	return db
}

func (u UserDB) Name(uid int) string {
	if uid < 0 {
		return "?"
	}
	if n, ok := u[uid]; ok {
		return n
	}
	return strconv.Itoa(uid)
}
