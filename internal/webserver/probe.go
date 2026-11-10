package webserver

import (
	"fmt"
	"net/http"
	"time"
)

// probeTimeout bounds the single-instance healthz probe — a hung peer
// must not stall the launch.
const probeTimeout = 1500 * time.Millisecond

// ProbeInstance is the single-instance check (spec §4): does the
// token-less /healthz on 127.0.0.1:port answer like our WebUI? Only an
// exact "ok" body counts — a foreign app on the port (or nothing at
// all) reports false and the launch scans upward past it. The probe
// reveals nothing about this instance and learns nothing about the
// running one beyond its existence.
func ProbeInstance(port int) bool {
	client := &http.Client{Timeout: probeTimeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	buf := make([]byte, 8) // "ok\n" plus slack; the body reveals nothing anyway
	n, _ := resp.Body.Read(buf)
	return string(buf[:n]) == "ok\n"
}
