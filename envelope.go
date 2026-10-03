package x12val

import "strconv"

// envelopeState tracks the open ISA/GS/ST envelopes and their counters.
// Only the innermost open envelope of each layer is kept.
type envelopeState struct {
	isaOpen  bool
	isa13    string
	gsCount  int
	gsOpen   bool
	gs06     string
	stCount  int
	stOpen   bool
	st02     string
	segCount int
}

// handleSegment applies envelope bookkeeping and validation for one
// complete segment, emitting events in a deterministic order.
func (v *validator) handleSegment(seg SegmentEvent) {
	switch seg.Tag {
	case "ISA":
		// A new ISA closes every layer of the previous interchange.
		v.closeST(seg.Offset)
		v.closeGS(seg.Offset)
		v.closeISA(seg.Offset)
		v.emitSegment(seg)
		v.env.isaOpen = true
		v.env.isa13 = elem(seg, 12)
		v.env.gsCount = 0
	case "GS":
		v.closeST(seg.Offset)
		v.closeGS(seg.Offset)
		v.emitSegment(seg)
		v.env.gsOpen = true
		v.env.gs06 = elem(seg, 5)
		v.env.stCount = 0
		if v.env.isaOpen {
			v.env.gsCount++
		}
	case "ST":
		v.closeST(seg.Offset)
		v.emitSegment(seg)
		v.env.stOpen = true
		v.env.st02 = elem(seg, 1)
		v.env.segCount = 1
		if v.env.gsOpen {
			v.env.stCount++
		}
	case "SE":
		if v.env.stOpen {
			v.env.segCount++
		}
		v.emitSegment(seg)
		if v.env.stOpen {
			v.checkCount(CodeSECountMismatch, seg.Offset, elem(seg, 0), v.env.segCount, "SE01")
			v.checkControl(CodeSEControlMismatch, seg.Offset, elem(seg, 1), v.env.st02, "SE02")
			v.env.stOpen = false
		}
	case "GE":
		v.closeST(seg.Offset)
		v.emitSegment(seg)
		if v.env.gsOpen {
			v.checkCount(CodeGECountMismatch, seg.Offset, elem(seg, 0), v.env.stCount, "GE01")
			v.checkControl(CodeGEControlMismatch, seg.Offset, elem(seg, 1), v.env.gs06, "GE02")
			v.env.gsOpen = false
		}
	case "IEA":
		v.closeST(seg.Offset)
		v.closeGS(seg.Offset)
		v.emitSegment(seg)
		if v.env.isaOpen {
			v.checkCount(CodeIEACountMismatch, seg.Offset, elem(seg, 0), v.env.gsCount, "IEA01")
			v.checkControl(CodeIEAControlMismatch, seg.Offset, elem(seg, 1), v.env.isa13, "IEA02")
			v.env.isaOpen = false
			// The next interchange declares its own delimiters.
			v.framer.hasDelim = false
		}
	default:
		v.emitSegment(seg)
		if v.env.stOpen {
			v.env.segCount++
		}
	}
}

// elem returns the raw value of element i (0-based) or "" if absent.
func elem(seg SegmentEvent, i int) string {
	if i < len(seg.Elements) {
		return seg.Elements[i].Raw
	}
	return ""
}

// checkCount compares a declared tail count with the actual count as
// decimal numbers.
func (v *validator) checkCount(code ErrorCode, offset int64, declared string, actual int, field string) {
	n, err := strconv.Atoi(declared)
	if err != nil || n != actual {
		v.emitError(ErrorEvent{
			Code:     code,
			Offset:   offset,
			Expected: declared,
			Actual:   strconv.Itoa(actual),
			Message:  field + " count mismatch",
		})
	}
}

// checkControl compares control numbers as raw bytes.
func (v *validator) checkControl(code ErrorCode, offset int64, got, want, field string) {
	if got != want {
		v.emitError(ErrorEvent{
			Code:     code,
			Offset:   offset,
			Expected: want,
			Actual:   got,
			Message:  field + " control number mismatch",
		})
	}
}

func (v *validator) closeST(offset int64) {
	if v.env.stOpen {
		v.emitError(ErrorEvent{Code: CodeMissingSE, Offset: offset, Message: "ST envelope missing SE"})
		v.env.stOpen = false
	}
}

func (v *validator) closeGS(offset int64) {
	if v.env.gsOpen {
		v.emitError(ErrorEvent{Code: CodeMissingGE, Offset: offset, Message: "GS envelope missing GE"})
		v.env.gsOpen = false
	}
}

func (v *validator) closeISA(offset int64) {
	if v.env.isaOpen {
		v.emitError(ErrorEvent{Code: CodeMissingIEA, Offset: offset, Message: "ISA envelope missing IEA"})
		v.env.isaOpen = false
	}
}

// finishEnvelope reports envelopes still open at end of input.
// totalLen is the total input length used as the error offset.
func (v *validator) finishEnvelope(totalLen int64) {
	v.closeST(totalLen)
	v.closeGS(totalLen)
	v.closeISA(totalLen)
}
