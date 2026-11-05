// Command fakeclaude stands in for a Claude Code process in the demo fleet: it only
// needs to stay alive under a name peekabot's liveness check accepts. With
// FAKECLAUDE_KEYLOG set it also appends every byte typed into its pane to that file,
// so the e2e check can see which option peekabot pressed on a permission prompt.
package main

import (
	"os"
	"time"
)

func main() {
	if log := os.Getenv("FAKECLAUDE_KEYLOG"); log != "" {
		go func() {
			b := make([]byte, 1)
			for {
				if n, err := os.Stdin.Read(b); err != nil {
					return
				} else if n == 1 {
					if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
						f.Write(b)
						f.Close()
					}
				}
			}
		}()
	}
	time.Sleep(24 * time.Hour)
}
