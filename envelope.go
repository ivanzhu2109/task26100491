package x12val

import (
	"fmt"
	"strconv"
)

// envelopeState tracks the currently open ISA/GS/ST envelopes. Counts and
// control numbers belong to the envelope that opened them and are reset
// whenever a new header of that layer arrives.
type envelopeState struct {
	isaOpen bool
	isaCtrl string // ISA13
	gsCount int    // GS headers inside the open ISA

	gsOpen  bool
	gsCtrl  string // GS06
	stCount int    // ST headers inside the open GS

	stOpen   bool
	stCtrl   string // ST02
	segCount int    // segments from ST up to the current one, inclusive
}

// elem returns the 0-based element value or "" when absent.
func elem(s *Segment, i int) string {
	if i < len(s.Elements) {
		return s.Elements[i].Value
	}
	return ""
}

// onISA handles a complete ISA segment (new interchange header).
func (v *Validator) onISA(seg *Segment) {
	o := seg.Offset
	if v.env.stOpen {
		v.emitError(ErrMissingSE, o, "ST envelope closed by new ISA")
		v.env.stOpen = false
	}
	if v.env.gsOpen {
		v.emitError(ErrMissingGE, o, "GS envelope closed by new ISA")
		v.env.gsOpen = false
	}
	if v.env.isaOpen {
		v.emitError(ErrMissingIEA, o, "ISA envelope closed by new ISA")
		v.env.isaOpen = false
	}
	v.env.isaOpen = true
	v.env.isaCtrl = elem(seg, 12)
	v.env.gsCount = 0
	v.emitSegment(seg)
}

// onSegment handles a complete non-ISA segment.
func (v *Validator) onSegment(seg *Segment) {
	switch seg.Tag {
	case "GS":
		v.onGS(seg)
	case "ST":
		v.onST(seg)
	case "SE":
		v.onSE(seg)
	case "GE":
		v.onGE(seg)
	case "IEA":
		v.onIEA(seg)
	default:
		if v.env.stOpen {
			v.env.segCount++
		}
		v.emitSegment(seg)
	}
}

func (v *Validator) onGS(seg *Segment) {
	o := seg.Offset
	if v.env.stOpen {
		v.emitError(ErrMissingSE, o, "ST envelope closed by new GS")
		v.env.stOpen = false
	}
	if v.env.gsOpen {
		v.emitError(ErrMissingGE, o, "GS envelope closed by new GS")
		v.env.gsOpen = false
	}
	v.env.gsOpen = true
	v.env.gsCtrl = elem(seg, 5)
	v.env.stCount = 0
	v.env.gsCount++
	v.emitSegment(seg)
}

func (v *Validator) onST(seg *Segment) {
	o := seg.Offset
	if v.env.stOpen {
		v.emitError(ErrMissingSE, o, "ST envelope closed by new ST")
		v.env.stOpen = false
	}
	v.env.stOpen = true
	v.env.stCtrl = elem(seg, 1)
	v.env.segCount = 1
	v.env.stCount++
	v.emitSegment(seg)
}

func (v *Validator) onSE(seg *Segment) {
	if v.env.stOpen {
		v.env.segCount++
	}
	v.emitSegment(seg)
	if v.env.stOpen {
		if got := elem(seg, 0); !countEqual(got, v.env.segCount) {
			v.emitError(ErrSECount, seg.Offset, fmt.Sprintf("SE01=%q actual=%d", got, v.env.segCount))
		}
		if got := elem(seg, 1); got != v.env.stCtrl {
			v.emitError(ErrSEControl, seg.Offset, fmt.Sprintf("SE02=%q ST02=%q", got, v.env.stCtrl))
		}
		v.env.stOpen = false
	}
}

func (v *Validator) onGE(seg *Segment) {
	o := seg.Offset
	if v.env.stOpen {
		v.emitError(ErrMissingSE, o, "ST envelope closed by GE")
		v.env.stOpen = false
	}
	v.emitSegment(seg)
	if v.env.gsOpen {
		if got := elem(seg, 0); !countEqual(got, v.env.stCount) {
			v.emitError(ErrGECount, seg.Offset, fmt.Sprintf("GE01=%q actual=%d", got, v.env.stCount))
		}
		if got := elem(seg, 1); got != v.env.gsCtrl {
			v.emitError(ErrGEControl, seg.Offset, fmt.Sprintf("GE02=%q GS06=%q", got, v.env.gsCtrl))
		}
		v.env.gsOpen = false
	}
}

func (v *Validator) onIEA(seg *Segment) {
	o := seg.Offset
	if v.env.stOpen {
		v.emitError(ErrMissingSE, o, "ST envelope closed by IEA")
		v.env.stOpen = false
	}
	if v.env.gsOpen {
		v.emitError(ErrMissingGE, o, "GS envelope closed by IEA")
		v.env.gsOpen = false
	}
	v.emitSegment(seg)
	if v.env.isaOpen {
		if got := elem(seg, 0); !countEqual(got, v.env.gsCount) {
			v.emitError(ErrIEACount, seg.Offset, fmt.Sprintf("IEA01=%q actual=%d", got, v.env.gsCount))
		}
		if got := elem(seg, 1); got != v.env.isaCtrl {
			v.emitError(ErrIEAControl, seg.Offset, fmt.Sprintf("IEA02=%q ISA13=%q", got, v.env.isaCtrl))
		}
		v.env.isaOpen = false
	}
	v.haveISA = false
}

// finishEnvelopes reports every envelope layer still open at EOF.
func (v *Validator) finishEnvelopes(offset int64) {
	if v.env.stOpen {
		v.emitError(ErrMissingSE, offset, "ST envelope not closed at EOF")
		v.env.stOpen = false
	}
	if v.env.gsOpen {
		v.emitError(ErrMissingGE, offset, "GS envelope not closed at EOF")
		v.env.gsOpen = false
	}
	if v.env.isaOpen {
		v.emitError(ErrMissingIEA, offset, "ISA envelope not closed at EOF")
		v.env.isaOpen = false
	}
}

// countEqual compares a trailer count field with the actual count as
// decimal numbers (leading zeros are insignificant).
func countEqual(got string, actual int) bool {
	n, err := strconv.Atoi(got)
	return err == nil && n == actual
}
