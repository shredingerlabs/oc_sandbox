package launcher

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// StopContainers is the graceful stop flow for the given oc-sandbox
// containers (pod-per-workspace #125): stop the owning pod as the unit
// (a pod stop takes the member containers down; pre-upgrade containers
// without a pod stop alone), then flip container_status="stopped" only
// after a fresh podman ps verifies they are gone (data map §3). emit
// streams the podman output lines. The quit prompt's stop uses this one
// flow (spec §4).
func StopContainers(run cmdrun.Runner, reg *registry.Registry, names []string, emit func(string)) error {
	for _, name := range names {
		// pod-per-workspace (#125): a container running inside a pod
		// takes its companions down with it — stop the pod as the
		// unit; pre-upgrade containers without a pod stop alone.
		pod := registry.PodName(registry.ContainerID(name))
		if discovery.PodExists(run, pod) {
			out, err := run.Output("podman", "pod", "stop", pod)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("podman pod stop %s failed: %s", pod, strings.TrimSpace(out))
			}
			continue
		}
		out, err := run.Output("podman", "stop", name)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return fmt.Errorf("podman stop %s failed: %s", name, strings.TrimSpace(out))
		}
	}
	// container_status="stopped" only after verified gone (data map
	// §3): reconcile against a fresh podman ps before any write.
	remaining := discovery.RunningContainers(run)
	for _, name := range names {
		if slices.Contains(remaining, name) {
			return fmt.Errorf("%s is still running", name)
		}
	}
	if reg == nil {
		return nil
	}
	for _, name := range names {
		id := strings.TrimPrefix(name, registry.ContainerName(""))
		p, err := reg.ByID(id)
		if err != nil {
			continue // not a registry workspace
		}
		if err := reg.UpdateStatus(p.Path, "stopped"); err != nil {
			return err
		}
	}
	return nil
}
