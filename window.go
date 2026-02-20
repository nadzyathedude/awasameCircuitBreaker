package circuitbreaker

// outcome represents the result of a single call.
type outcome bool

const (
	success outcome = true
	failure outcome = false
)

// slidingWindow is a fixed-size ring buffer that tracks call outcomes.
type slidingWindow struct {
	buf   []outcome
	pos   int  // next write position
	count int  // number of recorded outcomes (up to len(buf))
	fails int  // number of failures currently in the window
}

// newSlidingWindow creates a sliding window with the given capacity.
func newSlidingWindow(size int) *slidingWindow {
	if size <= 0 {
		size = 1
	}
	return &slidingWindow{
		buf: make([]outcome, size),
	}
}

// record adds an outcome to the window. When the buffer is full,
// the oldest entry is overwritten.
func (w *slidingWindow) record(o outcome) {
	if w.count == len(w.buf) {
		// Overwriting oldest entry — adjust fails count.
		old := w.buf[w.pos]
		if old == failure {
			w.fails--
		}
	} else {
		w.count++
	}

	w.buf[w.pos] = o
	if o == failure {
		w.fails++
	}

	w.pos = (w.pos + 1) % len(w.buf)
}

// failureRate returns the ratio of failures to total recorded outcomes.
// Returns 0 if no outcomes have been recorded.
func (w *slidingWindow) failureRate() float64 {
	if w.count == 0 {
		return 0
	}
	return float64(w.fails) / float64(w.count)
}

// total returns the number of outcomes currently in the window.
func (w *slidingWindow) total() int {
	return w.count
}

// reset clears all recorded outcomes.
func (w *slidingWindow) reset() {
	w.pos = 0
	w.count = 0
	w.fails = 0
}
