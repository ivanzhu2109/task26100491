// Package x12val provides streaming framing and envelope validation for
// X12 EDI interchanges. Only version 00501 is supported; business fields
// inside transaction bodies are not validated.
package x12val

// EventKind distinguishes segment events from error events.
type EventKind int

const (
	// EventSegment is emitted for every complete segment.
	EventSegment EventKind = iota
	// EventError is emitted for every validation or framing error.
	EventError
)

// ErrorCode is a stable machine-readable error identifier.
type ErrorCode string

const (
	CodeTruncatedISA       ErrorCode = "TRUNCATED_ISA"
	CodeTruncatedSegment   ErrorCode = "TRUNCATED_SEGMENT"
	CodeMissingSE          ErrorCode = "MISSING_SE"
	CodeMissingGE          ErrorCode = "MISSING_GE"
	CodeMissingIEA         ErrorCode = "MISSING_IEA"
	CodeSECountMismatch    ErrorCode = "SE_COUNT_MISMATCH"
	CodeGECountMismatch    ErrorCode = "GE_COUNT_MISMATCH"
	CodeIEACountMismatch   ErrorCode = "IEA_COUNT_MISMATCH"
	CodeSEControlMismatch  ErrorCode = "SE_CONTROL_MISMATCH"
	CodeGEControlMismatch  ErrorCode = "GE_CONTROL_MISMATCH"
	CodeIEAControlMismatch ErrorCode = "IEA_CONTROL_MISMATCH"
	CodeInvalidISA         ErrorCode = "INVALID_ISA"
	CodeExpectedISA        ErrorCode = "EXPECTED_ISA"
)

// Element is one parsed element of a segment. Raw is the unmodified text
// between element separators. Repeats holds the repetition/component
// decomposition for non-ISA segments; it is nil for the ISA segment, whose
// fields are only split at the element level.
type Element struct {
	Raw     string
	Repeats [][]string
}

// SegmentEvent describes one complete segment.
type SegmentEvent struct {
	Tag      string
	Elements []Element
	// Offset is the absolute 0-based byte offset of the segment start,
	// counting any CR/LF bytes between segments.
	Offset int64
}

// ErrorEvent describes one validation or framing error.
type ErrorEvent struct {
	Code ErrorCode
	// Offset is the segment-start offset of the triggering segment, or the
	// total input length for errors reported at EOF.
	Offset   int64
	Expected string
	Actual   string
	Message  string
}

// Event is the unit delivered to the emit callback. Exactly one of Segment
// and Error is non-nil, selected by Kind.
type Event struct {
	Kind    EventKind
	Segment *SegmentEvent
	Error   *ErrorEvent
}

// Validator consumes an X12 byte stream in arbitrary chunks.
type Validator interface {
	// Feed processes one chunk of input. Chunk boundaries need not align
	// with segment boundaries.
	Feed(chunk []byte)
	// Finish signals end of input and reports truncated segments and any
	// envelopes still open.
	Finish()
}

// NewValidator returns a Validator that delivers events synchronously to
// emit. The same byte stream always produces the same event sequence
// regardless of how it is chunked across Feed calls.
func NewValidator(emit func(Event)) Validator {
	v := &validator{emit: emit}
	v.framer.onSegment = v.handleSegment
	v.framer.onError = v.emitError
	return v
}
