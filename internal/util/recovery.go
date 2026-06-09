package util

import (
	"log"
	"runtime/debug"
)

// SafeGo runs the provided function inside a new goroutine wrapped in a recovery barrier.
// This prevents panic-induced termination of the core application process.
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC RECOVERY] Recovered from panic: %v\nStack trace:\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}
