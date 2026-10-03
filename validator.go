package x12val

// validator glues the framer and the envelope tracker to the public API.
type validator struct {
	emit   func(Event)
	framer framer
	env    envelopeState
}

func (v *validator) Feed(chunk []byte) {
	v.framer.feed(chunk)
}

func (v *validator) Finish() {
	v.framer.finish(v.framerTotal())
	v.finishEnvelope(v.framerTotal())
}

func (v *validator) framerTotal() int64 {
	return v.framer.pos
}

func (v *validator) emitSegment(seg SegmentEvent) {
	v.emit(Event{Kind: EventSegment, Segment: &seg})
}

func (v *validator) emitError(err ErrorEvent) {
	v.emit(Event{Kind: EventError, Error: &err})
}
