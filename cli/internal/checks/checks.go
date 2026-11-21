// Package checks refuses a request before any prompt is shown.
// The design is in design.md.
package checks

import (
	"unicode"
	"unicode/utf8"

	"github.com/kdop/gangway/cli/internal/request"
)

// ReasonControlCharacter is sent with the refusal. It is part of the
// contract in architecture.md.
const ReasonControlCharacter = "control_character"

// Check tells whether the request may go on to the line. When it may not,
// reason says why. The refused text is never part of the reason.
func Check(req request.Request) (reason string, ok bool) {
	if !utf8.ValidString(req.Command) {
		return ReasonControlCharacter, false
	}
	for _, r := range req.Command {
		if isControl(r) || isHidden(r) {
			return ReasonControlCharacter, false
		}
	}
	return "", true
}

func isControl(r rune) bool {
	switch {
	case r == '\n', r == '\t':
		return false
	case r < 0x20:
		return true
	case r >= 0x7f && r <= 0x9f:
		return true
	}
	return false
}

// isHidden tells whether the character is invisible or changes the reading
// order: the Unicode format class, and the line and paragraph separators.
func isHidden(r rune) bool {
	return unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}
