//go:build !linux

package ai

import (
	"sync/atomic"
)

// startMemWatch is a no-op outside Linux: /proc scanning is unavailable, so
// only the timeout + output cap + file-size cap apply there.
func startMemWatch(pgid int, budgetBytes int64, stop <-chan struct{}, killed *atomic.Bool) {
}
