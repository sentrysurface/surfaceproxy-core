package pruning

import (
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// GetBuffer obtains a reset bytes.Buffer from the pool.
func GetBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutBuffer returns a bytes.Buffer to the pool if it hasn't grown too large.
func PutBuffer(buf *bytes.Buffer) {
	// Do not retain buffers that grew excessively large to prevent memory bloat
	if buf.Cap() <= 65536 {
		bufferPool.Put(buf)
	}
}
