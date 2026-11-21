package runner

import (
	"strings"
	"unicode/utf8"
)

// mark stands in for bytes that are not text.
const mark = "�"

// text turns pieces of output into valid UTF-8. A character that arrives
// split over two pieces is held back until the rest comes.
type text struct {
	held     []byte
	replaced bool
}

func (t *text) add(piece []byte) string {
	data := append(t.held, piece...)
	t.held = nil
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax+1; i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				t.held = append([]byte(nil), data[i:]...)
				data = data[:i]
			}
			break
		}
	}
	return t.valid(data)
}

// flush gives what is still held back, when no more output will come.
func (t *text) flush() string {
	data := t.held
	t.held = nil
	return t.valid(data)
}

func (t *text) valid(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	t.replaced = true
	return strings.ToValidUTF8(string(data), mark)
}
