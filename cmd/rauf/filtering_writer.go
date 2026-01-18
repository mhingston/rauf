package main

import (
	"bytes"
	"io"
	"strings"
)

// filteringWriter wraps an io.Writer and skips lines matching a prefix.
// It buffers partial lines until a newline is found.
type filteringWriter struct {
	w      io.Writer
	prefix string
	buf    bytes.Buffer
}

func newFilteringWriter(w io.Writer, prefix string) *filteringWriter {
	return &filteringWriter{w: w, prefix: prefix}
}

func (fw *filteringWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	fw.buf.Write(p)

	for {
		b := fw.buf.Bytes()
		i := bytes.IndexByte(b, '\n')
		if i == -1 {
			// No complete line yet, wait for more data
			return n, nil
		}

		// Extract the line including the newline
		line := b[:i+1]
		lineStr := string(line)

		// Check if we should filter this line
		if !strings.HasPrefix(strings.TrimSpace(lineStr), fw.prefix) {
			if _, err := fw.w.Write(line); err != nil {
				return 0, err
			}
		}

		// Advance the buffer past the processed line
		// To do this efficiently with bytes.Buffer, we can read it out or discard.
		// Next(m) discards the next m bytes.
		fw.buf.Next(i + 1)
	}
}
