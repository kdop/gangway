// Package log writes each step of a request to the log of the session:
// the request, the answer, the output and the end. The design is in
// docs/design.md.
package log

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kdop/gangway/cli/internal/checks"
	"github.com/kdop/gangway/cli/internal/request"
	"github.com/kdop/gangway/cli/internal/runner"
	"github.com/kdop/gangway/shared/messages"
)

const (
	// Every line of an entry after its first starts with two spaces. The
	// first line starts with the time, so a command or an output cannot
	// write a line that reads as the start of an entry.
	commandMark = "  > "
	outputMark  = "  | "
	cutNote     = "  [cut: the log holds no more of this entry]\n"

	folderStart = "gangway-"
	name        = "session"
	ending      = ".txt"

	// mark stands in for a character that must not reach a screen.
	mark = '�'
	// longestID is the most of a request's ID that is written.
	longestID = 64
)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Place tells where the logs of sessions are made.
func Place() string { return os.TempDir() }

// Log is the log of one session. It is not safe for use from more than
// one goroutine: the one loop of the cli-tool owns it.
type Log struct {
	folder string
	size   int64 // LOG_FILE_SIZE
	kept   int   // LOG_FILES_KEPT
	clock  Clock

	file    *os.File
	written int64 // bytes in the file
	old     int   // old files in the folder
	err     error // the first failure. Nothing is written after it
	closed  bool

	// The output of the commands that run, by the ID of the request.
	output map[string]*strings.Builder
}

// Open makes a new folder in place, with a name nobody can guess, and the
// log file in it. Only the owner can read or write either.
func Open(place string, size int64, kept int, clock Clock) (*Log, error) {
	folder, err := os.MkdirTemp(place, folderStart)
	if err != nil {
		return nil, err
	}
	l := &Log{folder: folder, size: size, kept: kept, clock: clock, output: map[string]*strings.Builder{}}
	if err := l.create(); err != nil {
		os.Remove(folder)
		return nil, err
	}
	return l, nil
}

// File is the path of the log file. The newest entries are in it.
func (l *Log) File() string { return filepath.Join(l.folder, name+ending) }

// oldFile is the path of an old log file. Number 1 is the newest of them.
func (l *Log) oldFile(number int) string {
	return filepath.Join(l.folder, name+"."+strconv.Itoa(number)+ending)
}

// Old tells how many old log files are kept beside the log file.
func (l *Log) Old() int { return l.old }

// Err gives the first failure to write. After it the log is not whole.
func (l *Log) Err() error { return l.err }

// Close closes the log file. Nothing is deleted.
func (l *Log) Close() {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	l.closed = true
}

// Session writes one line about the session itself.
func (l *Log) Session(text string) {
	l.write("session "+oneLine(text), "", "")
}

// Request writes a request that came in. It has passed no check yet.
func (l *Log) Request(req request.Request) {
	l.write(id(req.ID)+" request", commandMark, req.Command)
}

// Started writes that the user pressed y for the request.
func (l *Log) Started(requestID string) {
	l.write(id(requestID)+" approved", "", "")
	if l.output[requestID] == nil {
		l.output[requestID] = &strings.Builder{}
	}
}

// Refused writes that the request will not run, and why.
func (l *Log) Refused(requestID, reason string) {
	l.write(id(requestID)+" refused: "+oneLine(reason), "", "")
}

// Output holds a piece of what the command wrote, as it was sent to the
// AI. It is written with the end of the command.
func (l *Log) Output(requestID, text string) {
	b := l.output[requestID]
	// More than one file's worth is never written, so it is not held.
	if b == nil || int64(b.Len()) > l.size {
		return
	}
	b.WriteString(text)
}

// Ended writes how the command ended, and its output.
func (l *Log) Ended(requestID string, result runner.Result) {
	how := oneLine(result.How)
	if result.How == messages.HowExit {
		how = "exit code " + strconv.Itoa(result.ExitCode)
	}
	if result.BytesReplaced {
		how += ", bytes that are not text were replaced"
	}
	var output string
	if b := l.output[requestID]; b != nil {
		output = b.String()
		delete(l.output, requestID)
	}
	l.write(id(requestID)+" ended: "+how, outputMark, output)
}

// write adds one entry: a first line with the time, then the body with
// lineMark at the start of each line. The entry goes into one file, whole.
func (l *Log) write(first, lineMark, body string) {
	if l.err != nil || l.closed {
		return
	}
	entry := l.entry(first, lineMark, body)
	if l.written > 0 && l.written+int64(len(entry)) > l.size {
		if l.err = l.turn(); l.err != nil {
			return
		}
	}
	n, err := l.file.WriteString(entry)
	l.written += int64(n)
	l.err = err
}

// entry builds the text of one entry. It is never longer than
// LOG_FILE_SIZE: a longer body is cut, with a note.
func (l *Log) entry(first, lineMark, body string) string {
	var b strings.Builder
	b.WriteString(l.clock.Now().Format(time.RFC3339))
	b.WriteByte(' ')
	b.WriteString(first)
	b.WriteByte('\n')
	if body == "" {
		return b.String()
	}
	room := l.size - int64(b.Len()) - int64(len(cutNote))
	midLine := false
	for _, r := range body {
		if checks.Forbidden(r) {
			r = mark
		}
		need := utf8.RuneLen(r)
		if !midLine {
			need += len(lineMark)
		}
		// One byte stays free for the line break that ends the body.
		if room-int64(need) < 1 {
			if midLine {
				b.WriteByte('\n')
			}
			b.WriteString(cutNote)
			return b.String()
		}
		room -= int64(need)
		if !midLine {
			b.WriteString(lineMark)
			midLine = true
		}
		b.WriteRune(r)
		if r == '\n' {
			midLine = false
		}
	}
	if midLine {
		b.WriteByte('\n')
	}
	return b.String()
}

// turn starts a new log file. The full one is kept under another name,
// and the oldest of the old ones is deleted when there are too many.
func (l *Log) turn() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	l.file = nil
	if l.old >= l.kept {
		if err := os.Remove(l.oldFile(l.old)); err != nil {
			return err
		}
		l.old--
	}
	for number := l.old; number >= 1; number-- {
		if err := os.Rename(l.oldFile(number), l.oldFile(number+1)); err != nil {
			return err
		}
	}
	if l.kept > 0 {
		if err := os.Rename(l.File(), l.oldFile(1)); err != nil {
			return err
		}
		l.old++
	} else if err := os.Remove(l.File()); err != nil {
		return err
	}
	return l.create()
}

// create makes the log file. It must not be there yet.
func (l *Log) create() error {
	file, err := os.OpenFile(l.File(), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	l.file, l.written = file, 0
	return nil
}

// id makes the ID of a request safe to write on a first line.
func id(s string) string {
	s = strings.Map(func(r rune) rune {
		if r <= ' ' || r > '~' {
			return '?'
		}
		return r
	}, s)
	if len(s) > longestID {
		s = s[:longestID]
	}
	if s == "" {
		return "?"
	}
	return s
}

// oneLine makes a text safe to write on a first line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if checks.Forbidden(r) {
			return mark
		}
		return r
	}, s)
}
