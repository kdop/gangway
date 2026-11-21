package terminal

import "unicode/utf8"

const (
	ctrlC  = 0x03
	escape = 0x1b

	// maxSequence caps a key that never ends.
	maxSequence = 32
)

// key is one key press, as the bytes the key sent.
type key struct {
	bytes []byte
	ctrlC bool
}

// parser cuts the bytes that come from the keyboard into key presses. A
// key that sends many bytes, such as an arrow, stays one key, also when
// its bytes come in two reads. When in doubt it joins bytes into one key:
// a joined key answers nothing, so a doubt can drop a key but never make
// one up.
type parser struct {
	pending  []byte // an Escape, or a sequence that is not over yet
	sequence bool   // pending is past the byte that follows the Escape
}

func (p *parser) feed(chunk []byte) []key {
	var keys []key
	for i := 0; i < len(chunk); i++ {
		b := chunk[i]
		switch {
		case b == ctrlC:
			// Ctrl+C is never eaten by an unfinished key.
			if p.pending != nil {
				keys = append(keys, key{bytes: p.take()})
			}
			keys = append(keys, key{bytes: []byte{b}, ctrlC: true})
		case p.pending != nil && !p.sequence:
			p.pending = append(p.pending, b)
			if b == '[' || b == 'O' {
				p.sequence = true
			} else {
				keys = append(keys, key{bytes: p.take()})
			}
		case p.pending != nil:
			p.pending = append(p.pending, b)
			if (b >= 0x40 && b <= 0x7e) || len(p.pending) >= maxSequence {
				keys = append(keys, key{bytes: p.take()})
			}
		case b == escape:
			p.pending = []byte{b}
		case b >= utf8.RuneSelf:
			// A letter of more than one byte, with the bytes that belong to it.
			end := i + 1
			for end < len(chunk) && !utf8.RuneStart(chunk[end]) {
				end++
			}
			keys = append(keys, key{bytes: append([]byte(nil), chunk[i:end]...)})
			i = end - 1
		default:
			keys = append(keys, key{bytes: []byte{b}})
		}
	}
	return keys
}

func (p *parser) take() []byte {
	b := p.pending
	p.pending = nil
	p.sequence = false
	return b
}
