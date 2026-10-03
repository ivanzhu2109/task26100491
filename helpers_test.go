package x12val

import (
	"fmt"
	"strings"
	"testing"
)

// padRight pads s with trailing spaces to exactly n bytes.
func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

// isaSegment builds a fixed-width 106-byte ISA segment (terminator
// included) with the given delimiters, sender/receiver IDs (ISA06/ISA08)
// and interchange control number (ISA13, must be 9 bytes).
func isaSegment(elem, rep, comp, term byte, sender, receiver, isa13 string) string {
	fields := []string{
		"00", padRight("", 10), // ISA01, ISA02
		"00", padRight("", 10), // ISA03, ISA04
		"ZZ", padRight(sender, 15), // ISA05, ISA06
		"ZZ", padRight(receiver, 15), // ISA07, ISA08
		"251003", "1200", // ISA09, ISA10
		string(rep), // ISA11
		"00501",     // ISA12
		isa13,       // ISA13
		"0", "P",    // ISA14, ISA15
		string(comp), // ISA16
	}
	seg := "ISA" + string(elem) + strings.Join(fields, string(elem)) + string(term)
	if len(seg) != isaLen {
		panic(fmt.Sprintf("ISA segment has wrong length %d", len(seg)))
	}
	return seg
}

// streamBuilder concatenates a stream while recording the absolute offset
// of every segment start.
type streamBuilder struct {
	s       strings.Builder
	offsets []int64
}

func (b *streamBuilder) seg(s string) {
	b.offsets = append(b.offsets, int64(b.s.Len()))
	b.s.WriteString(s)
}

func (b *streamBuilder) raw(s string) {
	b.s.WriteString(s)
}

func (b *streamBuilder) String() string { return b.s.String() }

// runChunks feeds stream through a fresh Validator using the given chunk
// sizes (a final remainder is flushed automatically) and returns all
// emitted events.
func runChunks(stream []byte, sizes ...int) []Event {
	var events []Event
	v := NewValidator(func(e Event) { events = append(events, e) })
	i := 0
	for _, sz := range sizes {
		if sz > len(stream)-i {
			sz = len(stream) - i
		}
		v.Feed(stream[i : i+sz])
		i += sz
	}
	if i < len(stream) {
		v.Feed(stream[i:])
	}
	v.Finish()
	return events
}

// eventKinds reduces events to a comparable "S:TAG" / "E:CODE" form.
func eventKinds(events []Event) []string {
	kinds := make([]string, len(events))
	for i, e := range events {
		if e.Segment != nil {
			kinds[i] = "S:" + e.Segment.Tag
		} else {
			kinds[i] = "E:" + e.Error.Code
		}
	}
	return kinds
}

func assertSameEvents(t *testing.T, want, got []Event, label string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: got %d events, want %d\ngot:  %v\nwant: %v",
			label, len(got), len(want), eventKinds(got), eventKinds(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		switch {
		case w.Segment != nil && g.Segment != nil:
			if !segmentEqual(w.Segment, g.Segment) {
				t.Fatalf("%s: event %d differs\n got: %+v\nwant: %+v", label, i, g.Segment, w.Segment)
			}
		case w.Error != nil && g.Error != nil:
			if *w.Error != *g.Error {
				t.Fatalf("%s: event %d differs\n got: %+v\nwant: %+v", label, i, g.Error, w.Error)
			}
		default:
			t.Fatalf("%s: event %d kind differs\n got: %+v\nwant: %+v", label, i, g, w)
		}
	}
}

func segmentEqual(a, b *Segment) bool {
	if a.Tag != b.Tag || a.Offset != b.Offset || len(a.Elements) != len(b.Elements) {
		return false
	}
	for i := range a.Elements {
		ae, be := a.Elements[i], b.Elements[i]
		if ae.Value != be.Value || len(ae.Repetitions) != len(be.Repetitions) {
			return false
		}
		for j := range ae.Repetitions {
			if !stringSliceEqual(ae.Repetitions[j], be.Repetitions[j]) {
				return false
			}
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
