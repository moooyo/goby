package server

import "sync"

// finishOriginalPrimaryWriter flushes exactly once while transport deadlines and
// the authorization lifetime are still owned. Failure precedes delivery marking;
// deferred cleanup cannot perform a second flush after a successful completion.
func finishOriginalPrimaryWriter(writer *idleResponseWriter, failed func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			completed := false
			defer func() {
				if !completed {
					failed()
				}
			}()
			writer.finish()
			completed = true
		})
	}
}
