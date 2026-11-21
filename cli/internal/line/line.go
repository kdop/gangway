// Package line holds the requests that wait for the prompt, oldest first.
// The design is in design.md.
package line

import "github.com/kdop/gangway/cli/internal/request"

// Line is not safe for use from more than one goroutine: the one loop of
// the cli-tool owns it.
type Line struct {
	max     int
	waiting []request.Request
}

// New makes an empty line. max is MAX_WAITING_REQUESTS.
func New(max int) *Line {
	return &Line{max: max}
}

// Add puts the request at the end. It gives false when the line is full.
// A request at the prompt counts toward the limit, so the caller says
// whether one is there.
func (l *Line) Add(req request.Request, atPrompt bool) bool {
	count := len(l.waiting)
	if atPrompt {
		count++
	}
	if count >= l.max {
		return false
	}
	l.waiting = append(l.waiting, req)
	return true
}

// Next takes out the oldest request.
func (l *Line) Next() (request.Request, bool) {
	if len(l.waiting) == 0 {
		return request.Request{}, false
	}
	req := l.waiting[0]
	l.waiting = l.waiting[1:]
	return req, true
}

// Remove takes out the request with this ID. It gives false when the
// request is not in the line.
func (l *Line) Remove(id string) bool {
	for i, req := range l.waiting {
		if req.ID == id {
			l.waiting = append(l.waiting[:i:i], l.waiting[i+1:]...)
			return true
		}
	}
	return false
}

// Empty takes out every request, oldest first.
func (l *Line) Empty() []request.Request {
	all := l.waiting
	l.waiting = nil
	return all
}
