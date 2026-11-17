package webserver

import (
	"io/fs"

	"github.com/shredingerlabs/oc-sandbox/web"
)

// assetsFS is the embedded frontend, already rooted at web/ by the
// web package's embed directive (repo-root web/ committed source).
var assetsFS fs.FS = web.Assets
