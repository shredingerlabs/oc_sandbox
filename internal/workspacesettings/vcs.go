package workspacesettings

import (
	"encoding/json"
	"os"
	"sort"
)

// ReadHost returns the first (sorted) host recorded in the tracker's CLI
// host credential file, ignoring glab's "editor" key. The WebUI
// workspace settings render prefills its Host field with it.
func ReadHost(tracker, root string) string {
	data, err := os.ReadFile(VCSHostFile(tracker, root))
	if err != nil {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	var keys []string
	for k := range doc {
		if k != "editor" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

// VCSHostFile resolves the CLI host credential file of a tracker (the
// replacement-confirm guard target): github.com → gh-cli/hosts.yml;
// gitlab.com / own GitLab → glab-cli/config.yml; others → vcs/hosts.yml.
func VCSHostFile(tracker, root string) string {
	var sub string
	switch tracker {
	case "github.com":
		sub = "gh-cli/hosts.yml"
	case "gitlab.com", "own GitLab":
		sub = "glab-cli/config.yml"
	case "others":
		sub = "vcs/hosts.yml"
	default:
		return ""
	}
	return root + "/.git_local/" + sub
}
