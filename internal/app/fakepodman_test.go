package app_test

// Repro harness helper: a fake podman binary on PATH so script-side
// invocations (build-container.sh) share state with model-side queries.
// `build -t NAME` creates $FAKE_PODMAN_STATE/NAME; `image exists NAME`
// succeeds only when that marker exists — so a build really flips the
// image-existence answer for both sides.

import (
	"os"
	"path/filepath"
	"testing"
)

func installFakePodman(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	state := t.TempDir()
	script := filepath.Join(bin, "podman")
	body := "#!/usr/bin/env bash\n" +
		"state=\"$FAKE_PODMAN_STATE\"\n" +
		"case \"$1\" in\n" +
		"  build)\n" +
		"    prev=\"\"; target=\"\"\n" +
		"    for a in \"$@\"; do [[ \"$prev\" == \"-t\" ]] && target=\"$a\"; prev=\"$a\"; done\n" +
		"    touch \"$state/$target\"\n" +
		"    exit 0;;\n" +
		"  image)\n" +
		"    [[ -f \"$state/$3\" ]] && exit 0\n" +
		"    exit 1;;\n" +
		"  ps|port)\n" +
		"    exit 1;;\n" +
		"  *)\n" +
		"    exit 0;;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_PODMAN_STATE", state)
}
