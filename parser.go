package x12val

import "strings"

// delimiters holds the four delimiters declared by one ISA segment.
type delimiters struct {
	element    byte
	repetition byte
	component  byte
	segment    byte
}

const (
	stBoundary = iota // between segments; CR/LF skipped here
	stPeek            // collecting the first 3 bytes of a segment
	stISA             // collecting a fixed 106-byte ISA segment
	stScan            // scanning for the current segment terminator
)

const isaLen = 106

// framer is the byte-level streaming state machine. It turns arbitrary
// chunks into complete raw segments and reports framing-level errors.
// Only the current partial segment is buffered.
type framer struct {
	onSegment func(SegmentEvent)
	onError   func(ErrorEvent)

	state    int
	pos      int64 // absolute offset of the next byte to consume
	segStart int64 // absolute offset of the current segment start
	buf      []byte
	delim    delimiters
	hasDelim bool
}

// feed consumes one chunk of input.
func (f *framer) feed(chunk []byte) {
	for _, b := range chunk {
		f.step(b)
		f.pos++
	}
}

func (f *framer) step(b byte) {
	switch f.state {
	case stBoundary:
		if b == '\r' || b == '\n' {
			return
		}
		f.segStart = f.pos
		f.buf = append(f.buf[:0], b)
		f.state = stPeek
	case stPeek:
		if f.hasDelim && b == f.delim.segment {
			// A segment shorter than 3 bytes, e.g. "GE~".
			f.emitRaw()
			f.buf = f.buf[:0]
			f.state = stBoundary
			return
		}
		f.buf = append(f.buf, b)
		if len(f.buf) < 3 {
			return
		}
		if string(f.buf[:3]) == "ISA" {
			f.state = stISA
			return
		}
		if !f.hasDelim {
			// Outside any interchange and not an ISA header: drop one
			// byte and keep looking for a segment start.
			f.onError(ErrorEvent{
				Code:    CodeExpectedISA,
				Offset:  f.segStart,
				Message: "expected ISA segment",
			})
			f.buf = f.buf[1:]
			f.segStart++
			return
		}
		f.state = stScan
	case stISA:
		f.buf = append(f.buf, b)
		if len(f.buf) == isaLen {
			f.processISA()
			f.buf = f.buf[:0]
			f.state = stBoundary
		}
	case stScan:
		if b == f.delim.segment {
			f.emitRaw()
			f.buf = f.buf[:0]
			f.state = stBoundary
			return
		}
		f.buf = append(f.buf, b)
	}
}

// finish reports a truncated partial segment, if any. totalLen is the
// total number of input bytes consumed.
func (f *framer) finish(totalLen int64) {
	switch f.state {
	case stISA:
		f.onError(ErrorEvent{
			Code:    CodeTruncatedISA,
			Offset:  totalLen,
			Message: "input ended inside ISA segment",
		})
	case stPeek, stScan:
		if len(f.buf) > 0 {
			f.onError(ErrorEvent{
				Code:    CodeTruncatedSegment,
				Offset:  totalLen,
				Message: "input ended inside segment",
			})
		}
	}
	f.buf = f.buf[:0]
	f.state = stBoundary
}

// processISA handles a complete 106-byte ISA segment in f.buf.
func (f *framer) processISA() {
	d := delimiters{
		element:    f.buf[3],
		repetition: f.buf[82],
		component:  f.buf[104],
		segment:    f.buf[105],
	}
	if string(f.buf[84:89]) != "00501" {
		f.onError(ErrorEvent{
			Code:    CodeInvalidISA,
			Offset:  f.segStart,
			Actual:  string(f.buf[84:89]),
			Message: "ISA12 is not 00501",
		})
		return
	}
	if !validDelimiters(d) {
		f.onError(ErrorEvent{
			Code:    CodeInvalidISA,
			Offset:  f.segStart,
			Message: "ISA delimiters are not distinct printable punctuation",
		})
		return
	}
	f.delim = d
	f.hasDelim = true
	// ISA fields are split only at the element level; ISA11 and ISA16 are
	// plain values here, not structure.
	parts := strings.Split(string(f.buf[4:isaLen-1]), string(d.element))
	elems := make([]Element, len(parts))
	for i, p := range parts {
		elems[i] = Element{Raw: p}
	}
	f.onSegment(SegmentEvent{Tag: "ISA", Elements: elems, Offset: f.segStart})
}

// emitRaw parses f.buf as a regular segment with the current delimiters
// and delivers it. Empty values are preserved at every level.
func (f *framer) emitRaw() {
	parts := strings.Split(string(f.buf), string(f.delim.element))
	seg := SegmentEvent{Tag: parts[0], Offset: f.segStart}
	if len(parts) > 1 {
		seg.Elements = make([]Element, len(parts)-1)
		for i, p := range parts[1:] {
			reps := strings.Split(p, string(f.delim.repetition))
			el := Element{Raw: p, Repeats: make([][]string, len(reps))}
			for j, r := range reps {
				el.Repeats[j] = strings.Split(r, string(f.delim.component))
			}
			seg.Elements[i] = el
		}
	}
	f.onSegment(seg)
}

func validDelimiters(d delimiters) bool {
	ds := [4]byte{d.element, d.repetition, d.component, d.segment}
	for i, c := range ds {
		if !isPunct(c) {
			return false
		}
		for _, o := range ds[i+1:] {
			if c == o {
				return false
			}
		}
	}
	return true
}

func isPunct(c byte) bool {
	switch {
	case c >= '!' && c <= '/':
		return true
	case c >= ':' && c <= '@':
		return true
	case c >= '[' && c <= '`':
		return true
	case c >= '{' && c <= '~':
		return true
	}
	return false
}
