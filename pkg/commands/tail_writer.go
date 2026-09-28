package commands

// tailWriter is an io.Writer that retains only the most recent maxBytes
// written to it. It exists so executeSecuredCommandIn can capture a render
// tool's combined stdout+stderr for logging without buffering the full
// stream: RLIMIT_FSIZE bounds regular files, not a pipe, so a process that
// only prints (rather than writes to a file it opens) can otherwise grow an
// in-memory buffer such as exec.Cmd.CombinedOutput without limit. Only the
// tail is kept, not the head, because a TeX run's fatal error is always its
// last lines; that is what a diagnosis needs.
//
// It relies on the os/exec guarantee that when Stdout and Stderr are set to
// the same comparable writer, at most one goroutine calls Write at a time,
// so it does not need its own lock.
type tailWriter struct {
	maxBytes int
	buf      []byte
	total    int64
}

func newTailWriter(maxBytes int) *tailWriter {
	return &tailWriter{maxBytes: maxBytes}
}

func (w *tailWriter) Write(chunk []byte) (int, error) {
	w.total += int64(len(chunk))
	w.buf = append(w.buf, chunk...)

	if len(w.buf) > w.maxBytes {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.maxBytes:]...)
	}

	return len(chunk), nil
}

// Bytes returns the retained tail and whether anything was discarded to
// keep it within maxBytes.
func (w *tailWriter) Bytes() ([]byte, bool) {
	return w.buf, w.total > int64(len(w.buf))
}
