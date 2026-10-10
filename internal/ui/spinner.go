package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"pigo/internal/config"
)

// Cursor control sequences. The spinner hides the cursor while it runs and
// restores it on Stop, so a cancelled, failed, or panicking turn never leaves
// the cursor hidden.
const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

// spinner paints a transient activity line on stderr. When anim is set a
// goroutine ticks while a request is in flight; Stop is synchronous so the
// line is cleared before any normal output is written.
type spinner struct {
	out      io.Writer
	anim     bool // stderr is a TTY and NO_COLOR is unset
	ascii    bool
	styles   styles
	interval time.Duration

	mu      sync.Mutex
	running bool
	start   time.Time
	stopc   chan struct{}
	donec   chan struct{}
}

func newSpinner(out io.Writer, anim bool, st styles, ascii bool) *spinner {
	return &spinner{out: out, anim: anim, ascii: ascii, styles: st,
		interval: time.Duration(config.SpinnerIntervalMs) * time.Millisecond}
}

// spinnerASCII reports whether to use the ASCII glyph fallback. Termux fonts
// lack braille, so the environment opts in.
func spinnerASCII() bool { return os.Getenv("TERMUX_VERSION") != "" }

// StartSpinner shows the activity line. It is a no-op when the spinner is
// disabled.
func (u *UI) StartSpinner() { u.spin.Start() }

// StopSpinner clears the activity line and waits for the renderer. It is safe
// to call when no spinner is running.
func (u *UI) StopSpinner() { u.spin.Stop() }

// DisableSpinner turns the spinner off for the rest of the run, used by -p
// mode so redirected stderr stays quiet.
func (u *UI) DisableSpinner() {
	u.spin.Stop()
	u.spin = nil
}

// Start shows the spinner. Calling Start while it runs is a no-op, so a
// repeated request cannot spawn a second ticker.
func (s *spinner) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.start = time.Now()
	if !s.anim {
		s.mu.Unlock()
		fmt.Fprintln(s.out, s.line(s.frames()[0])) // one static line off a TTY
		return
	}
	s.stopc = make(chan struct{})
	s.donec = make(chan struct{})
	stopc, donec := s.stopc, s.donec
	s.mu.Unlock()

	fmt.Fprint(s.out, hideCursor)
	go s.loop(stopc, donec)
}

// Stop clears the activity line and waits for the ticker to exit. Calling Stop
// when not running is a no-op, and a nil spinner succeeds.
func (s *spinner) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	if !s.anim {
		s.mu.Unlock()
		return
	}
	stopc, donec := s.stopc, s.donec
	s.mu.Unlock()
	close(stopc)
	<-donec
	fmt.Fprint(s.out, "\r\x1b[K"+showCursor)
}

// loop redraws the spinner every interval until stopc is closed.
func (s *spinner) loop(stopc, donec chan struct{}) {
	defer close(donec)
	frames := s.frames()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for i := 0; ; i++ {
		fmt.Fprint(s.out, "\r\x1b[K"+s.line(frames[i%len(frames)]))
		select {
		case <-stopc:
			return
		case <-t.C:
		}
	}
}

// frames returns the glyph set: ASCII on terminals whose font lacks braille.
func (s *spinner) frames() []rune {
	if s.ascii {
		return []rune(config.SpinnerASCIIFrames)
	}
	return []rune(config.SpinnerFrames)
}

// line formats the glyph and elapsed seconds. The glyph carries the accent
// color; the elapsed time is dim.
func (s *spinner) line(frame rune) string {
	elapsed := int(time.Since(s.start).Seconds())
	return s.styles.prompt.Render(string(frame)) +
		s.styles.dim.Render(fmt.Sprintf(" %ds", elapsed))
}
