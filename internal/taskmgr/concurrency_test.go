package taskmgr

import (
	"fmt"
	"sync"
	"testing"
)

// The manager is shared by the TUI event loop and — since the WebUI
// drives ops from HTTP-handler goroutines — concurrent callers. Launch,
// Update and the queries must be safe under -race.
func TestConcurrentLaunchUpdateQueries(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("w%d", n%2)
			cmd := m.Launch(Op{Key: id, Title: "op", Run: func(emit func(string)) error { return nil }})
			for cmd != nil {
				msg := cmd()
				if msg == nil {
					break
				}
				if next := m.Update(msg); next != nil {
					cmd = next
					continue
				}
				break
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = m.Busy()
				_ = m.BusyKey("w0")
				_, _ = m.RunningTitle("w1")
			}
		}()
	}
	wg.Wait()
}
