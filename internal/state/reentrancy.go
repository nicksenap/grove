package state

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// The state lock is a cross-process advisory (flock) lock. Within a single
// process, two goroutines opening it via separate file descriptors still
// contend, so they serialize correctly. The only case flock cannot distinguish
// is same-goroutine reentrancy, which would otherwise self-block for the full
// timeout. This registry tracks which goroutine currently holds each lock path
// so a nested call fails fast instead.

var (
	heldMu sync.Mutex
	held   = map[string]int64{} // lockPath -> owning goroutine id
)

func markHeld(lockPath string) {
	id, ok := goid()
	if !ok {
		return
	}
	heldMu.Lock()
	held[lockPath] = id
	heldMu.Unlock()
}

func clearHeld(lockPath string) {
	heldMu.Lock()
	delete(held, lockPath)
	heldMu.Unlock()
}

func heldByCurrentGoroutine(lockPath string) bool {
	id, ok := goid()
	if !ok {
		return false // parsing failed; fall back to flock-only behavior
	}
	heldMu.Lock()
	owner, held := held[lockPath]
	heldMu.Unlock()
	return held && owner == id
}

// goid returns the current goroutine id by parsing the runtime stack header
// ("goroutine <id> [running]:"). This is not an official API; on a parse
// failure it returns ok=false so callers fall back to flock-only serialization
// rather than risking a false reentrancy match.
func goid() (int64, bool) {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := string(buf[:n])
	s = strings.TrimPrefix(s, "goroutine ")
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
