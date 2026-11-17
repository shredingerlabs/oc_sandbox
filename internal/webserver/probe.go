package webserver

import (
	"fmt"
	"net/http"
	"time"
)

// probeTimeout bounds the single-instance healthz probe — a hung peer
// must not stall the launch.
const probeTimeout = 1500 * time.Millisecond

// FindInstance probes the whole port-scan window (BasePort..+scanWindow,
// the same range Start binds in) for a running oc-sandbox WebUI and
// returns its port — 0 when none answers. A foreign app on the base
// port alone would hide a displaced instance one port up; the second
// launch must point at the running instance, not scan past it (§4,
// story 10).
func FindInstance(base int) int {
	if base == 0 {
		base = BasePort
	}
	for port := base; port < base+scanWindow; port++ {
		if ProbeInstance(port) {
			return port
		}
	}
	return 0
}

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
