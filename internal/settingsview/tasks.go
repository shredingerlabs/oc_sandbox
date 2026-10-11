package settingsview

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// queueOp routes ONE long operation (uninstall script, export write,
// restore write — all through the Ops seam) through the shared
// background-task manager when wired (#82): enqueue a task; its
// continuation runs from the taskmgr.DoneMsg. Without a manager the op
// runs synchronously (legacy parity) and the continuation's tail cmd is
// returned directly.
func (v *View) queueOp(title string, run func() (string, error), after func(err error) tea.Cmd) tea.Cmd {
	if v.Tasks == nil {
		_, err := run()
		return after(err)
	}
	v.opSeq++
	v.queuePending[v.opSeq] = after
	return v.Tasks.Launch(taskmgr.Op{
		ID:    v.opSeq,
		Key:   "settings", // config-file ops serialize
		Title: title,
		Run: func(emit func(string)) error {
			_, err := run()
			return err
		},
	})
}
