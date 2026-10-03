// Package x12val provides streaming X12 (00501) framing and envelope validation.
package x12val

import (
	"bytes"
	"strings"
)

// isaLen is the fixed total length of an ISA segment, terminator included.
const isaLen = 106

// Segment is a fully parsed segment event.
type Segment struct {
	// Tag is the segment identifier (e.g. "ISA", "GS", "ST").
	Tag string
	// Elements holds the parsed elements after the tag.
	// For ISA, elements are split only by the element separator.
	// For other segments, elements are split by element, repetition and
	// component separators; see Element.
	Elements []Element
	// Offset is the absolute 0-based byte offset of the segment start.
	Offset int64
}

// Element is one element of a segment. For non-ISA segments it may be
// further split into repetitions, each of which may be split into
// components. For ISA, Repetitions always has exactly one entry with a
// single component (the raw value).
type Element struct {
	// Value is the raw element text (no trimming).
	Value string
	// Repetitions holds the element split by the repetition separator.
	// Each repetition is split by the component separator into components.
	// Empty values between/after separators are preserved.
	Repetitions [][]string
}

// Error codes emitted by the validator.
const (
	// ErrTruncatedSegment: input ended in the middle of a segment (EOF).
	ErrTruncatedSegment = "TRUNCATED_SEGMENT"
	// ErrMissingSE: an ST envelope was closed without its SE trailer.
	ErrMissingSE = "MISSING_SE"
	// ErrMissingGE: a GS envelope was closed without its GE trailer.
	ErrMissingGE = "MISSING_GE"
	// ErrMissingIEA: an ISA envelope was closed without its IEA trailer.
	ErrMissingIEA = "MISSING_IEA"
	// ErrSECount: SE01 does not equal the actual ST..SE segment count.
	ErrSECount = "SE_COUNT_MISMATCH"
	// ErrGECount: GE01 does not equal the actual number of ST headers.
	ErrGECount = "GE_COUNT_MISMATCH"
	// ErrIEACount: IEA01 does not equal the actual number of GS headers.
	ErrIEACount = "IEA_COUNT_MISMATCH"
	// ErrSEControl: SE02 does not match ST02.
	ErrSEControl = "SE_CONTROL_MISMATCH"
	// ErrGEControl: GE02 does not match GS06.
	ErrGEControl = "GE_CONTROL_MISMATCH"
	// ErrIEAControl: IEA02 does not match ISA13.
	ErrIEAControl = "IEA_CONTROL_MISMATCH"
)

// Error is a validation error event.
type Error struct {
	// Code is one of the stable Err* codes.
	Code string
	// Offset is the absolute 0-based byte offset of the segment that
	// triggered the error; for EOF errors it is the total input length.
	Offset int64
	// Detail carries human-readable context (e.g. expected vs actual).
	Detail string
}

// Event is delivered to the emit callback. Exactly one of Segment or
// Error is non-nil.
type Event struct {
	Segment *Segment
	Error   *Error
}

// Validator is a streaming X12 envelope validator. It is not safe for
// concurrent use.
//
// Event ordering: when an incoming segment implicitly closes open
// envelopes, the missing-trailer errors (innermost layer first) are
// emitted before the segment event; a trailer's own count/control
// mismatches are emitted right after its segment event.
type Validator struct {
	emit func(Event)

	buf   []byte // unprocessed bytes
	base  int64  // absolute offset of buf[0]
	total int64  // total bytes fed

	haveISA bool // delimiters of the current interchange are known
	elemSep byte
	repSep  byte
	compSep byte
	segTerm byte

	env envelopeState
}

// NewValidator creates a Validator that synchronously delivers events to
// emit. emit must not call back into the Validator.
func NewValidator(emit func(Event)) *Validator {
	return &Validator{emit: emit}
}

// Feed consumes a chunk of the input stream. Chunks may split the stream
// at arbitrary byte boundaries; the emitted event sequence is identical
// for any chunking of the same byte stream.
func (v *Validator) Feed(chunk []byte) {
	v.total += int64(len(chunk))
	v.buf = append(v.buf, chunk...)
	v.process()
}

// Finish signals end of input. It reports a truncated trailing segment
// (if any) and every envelope layer still open.
func (v *Validator) Finish() {
	v.skipCRLF()
	if len(v.buf) > 0 {
		v.emitError(ErrTruncatedSegment, v.total, "input ended mid-segment")
		v.buf = v.buf[:0]
	}
	v.finishEnvelopes(v.total)
}

// process drains complete segments from buf, stopping when the remaining
// bytes form only a partial segment.
func (v *Validator) process() {
	for {
		v.skipCRLF()
		if len(v.buf) == 0 {
			return
		}
		if v.atISA() {
			if len(v.buf) < isaLen {
				v.compact()
				return
			}
			raw := v.buf[:isaLen]
			v.elemSep, v.repSep, v.compSep, v.segTerm = raw[3], raw[82], raw[104], raw[105]
			seg := parseISA(raw, v.base)
			v.consume(isaLen)
			v.haveISA = true
			v.onISA(seg)
			continue
		}
		idx := bytes.IndexByte(v.buf, v.segTerm)
		if idx < 0 {
			v.compact()
			return
		}
		seg := parseSegment(v.buf[:idx], v.base, v.elemSep, v.repSep, v.compSep)
		v.consume(idx + 1)
		v.onSegment(seg)
	}
}

// atISA reports whether the buffered bytes start a new ISA segment: either
// no interchange is currently open (stream start or just after IEA), or a
// segment boundary begins with "ISA" (recovery after a missing IEA). The
// check only applies at segment boundaries, never mid-segment.
func (v *Validator) atISA() bool {
	if !v.haveISA {
		return true
	}
	return len(v.buf) >= 3 && v.buf[0] == 'I' && v.buf[1] == 'S' && v.buf[2] == 'A'
}

// skipCRLF consumes inter-segment CR/LF bytes.
func (v *Validator) skipCRLF() {
	n := 0
	for n < len(v.buf) && (v.buf[n] == '\r' || v.buf[n] == '\n') {
		n++
	}
	if n > 0 {
		v.consume(n)
	}
}

// consume drops n processed bytes from the front of buf.
func (v *Validator) consume(n int) {
	v.base += int64(n)
	v.buf = v.buf[n:]
	if len(v.buf) == 0 {
		v.buf = v.buf[:0]
	}
}

// compact releases the consumed prefix of buf once it grows large, keeping
// live memory proportional to the pending partial segment only.
func (v *Validator) compact() {
	if cap(v.buf) > 1<<16 && len(v.buf) < cap(v.buf)/2 {
		nb := make([]byte, len(v.buf))
		copy(nb, v.buf)
		v.buf = nb
	}
}

// parseISA parses a fixed-width ISA segment. Elements are split only by
// the element separator; ISA11/ISA16 carry delimiters, not structure.
func parseISA(raw []byte, offset int64) *Segment {
	elem := raw[3]
	parts := strings.Split(string(raw[4:isaLen-1]), string(elem))
	els := make([]Element, len(parts))
	for i, p := range parts {
		els[i] = Element{Value: p, Repetitions: [][]string{{p}}}
	}
	return &Segment{Tag: "ISA", Elements: els, Offset: offset}
}

// parseSegment parses a non-ISA segment into tag and elements, splitting
// by element, repetition and component separators and preserving empty
// values at every level.
func parseSegment(raw []byte, offset int64, elem, rep, comp byte) *Segment {
	s := string(raw)
	seg := &Segment{Offset: offset}
	body := ""
	if i := strings.IndexByte(s, elem); i >= 0 {
		seg.Tag = s[:i]
		body = s[i+1:]
	} else {
		seg.Tag = s
		return seg
	}
	parts := strings.Split(body, string(elem))
	seg.Elements = make([]Element, len(parts))
	for i, p := range parts {
		reps := strings.Split(p, string(rep))
		rs := make([][]string, len(reps))
		for j, r := range reps {
			rs[j] = strings.Split(r, string(comp))
		}
		seg.Elements[i] = Element{Value: p, Repetitions: rs}
	}
	return seg
}

func (v *Validator) emitSegment(s *Segment) {
	v.emit(Event{Segment: s})
}

func (v *Validator) emitError(code string, offset int64, detail string) {
	v.emit(Event{Error: &Error{Code: code, Offset: offset, Detail: detail}})
}
