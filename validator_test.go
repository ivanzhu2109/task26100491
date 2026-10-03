package x12val

import (
	"math/rand"
	"testing"
)

// buildTwoInterchanges builds a stream of two concatenated interchanges
// with different delimiters. The second ISA contains the first
// interchange's terminator inside ISA06. Data includes the literal text
// "ISA" inside a normal element, repeated elements with a trailing empty
// repetition, and a trailing empty component. CR/LF appears between some
// segments.
func buildTwoInterchanges() (string, []int64) {
	var b streamBuilder

	// Interchange A: elem '*' rep '^' comp ':' term '~'
	b.seg(isaSegment('*', '^', ':', '~', "SENDER", "RECEIVER", "000000001"))
	b.raw("\r\n")
	b.seg("GS*HC*SEND*RECV*20251003*1200*1*X*005010X222~")
	b.seg("ST*837*0001~")
	b.raw("\n")
	b.seg("NM1*IL*1*ISA*SMITH~") // element 3 is the literal text "ISA"
	b.seg("REF*1L*A^B^~")        // repetitions "A","B","" (trailing empty)
	b.seg("SVC*HC:99213:*1~")    // components "HC","99213","" (trailing empty)
	b.seg("SE*5*0001~")
	b.seg("GE*1*1~")
	b.seg("IEA*1*000000001~")
	b.raw("\r\n")

	// Interchange B: elem '|' rep '?' comp '%' term '!'
	// ISA06 "SE~NDER" contains interchange A's terminator '~'.
	b.seg(isaSegment('|', '?', '%', '!', "SE~NDER", "RECV2", "000000777"))
	b.seg("GS|HC|SEND|RECV|20251003|1300|7|X|005010X222!")
	b.seg("ST|837|0002!")
	b.seg("NM1|IL|1|DOE!")
	b.seg("SE|3|0002!")
	b.seg("GE|1|7!")
	b.seg("IEA|1|000000777!")

	return b.String(), b.offsets
}

func TestConcatenatedInterchanges(t *testing.T) {
	stream, offsets := buildTwoInterchanges()
	events := runChunks([]byte(stream))

	wantTags := []string{
		"ISA", "GS", "ST", "NM1", "REF", "SVC", "SE", "GE", "IEA",
		"ISA", "GS", "ST", "NM1", "SE", "GE", "IEA",
	}
	if len(events) != len(wantTags) {
		t.Fatalf("got %d events, want %d: %v", len(events), len(wantTags), eventKinds(events))
	}
	for i, tag := range wantTags {
		seg := events[i].Segment
		if seg == nil {
			t.Fatalf("event %d: got error %+v, want segment %s", i, events[i].Error, tag)
		}
		if seg.Tag != tag {
			t.Errorf("event %d: tag = %q, want %q", i, seg.Tag, tag)
		}
		if seg.Offset != offsets[i] {
			t.Errorf("event %d (%s): offset = %d, want %d", i, tag, seg.Offset, offsets[i])
		}
	}

	// Hand-determined expectations on parsed content.
	isa1 := events[0].Segment
	if got := len(isa1.Elements); got != 16 {
		t.Fatalf("ISA1: got %d elements, want 16", got)
	}
	if got := isa1.Elements[10].Value; got != "^" {
		t.Errorf("ISA11 = %q, want %q (treated as plain value)", got, "^")
	}
	if got := isa1.Elements[11].Value; got != "00501" {
		t.Errorf("ISA12 = %q, want %q", got, "00501")
	}
	if got := isa1.Elements[12].Value; got != "000000001" {
		t.Errorf("ISA13 = %q, want %q", got, "000000001")
	}
	if got := isa1.Elements[15].Value; got != ":" {
		t.Errorf("ISA16 = %q, want %q (treated as plain value)", got, ":")
	}
	if got := isa1.Elements[5].Value; got != "SENDER         " {
		t.Errorf("ISA06 = %q, want padded %q (no trimming)", got, "SENDER         ")
	}

	nm1 := events[3].Segment
	if got := nm1.Elements[2].Value; got != "ISA" {
		t.Errorf("NM103 = %q, want %q", got, "ISA")
	}

	ref := events[4].Segment
	wantReps := [][]string{{"A"}, {"B"}, {""}}
	if !segmentEqual(&Segment{Tag: "REF", Elements: []Element{{Value: "A^B^", Repetitions: wantReps}}},
		&Segment{Tag: "REF", Elements: []Element{ref.Elements[1]}}) {
		t.Errorf("REF02 repetitions = %v, want %v", ref.Elements[1].Repetitions, wantReps)
	}

	svc := events[5].Segment
	wantComps := [][]string{{"HC", "99213", ""}}
	if !stringSliceEqual(svc.Elements[0].Repetitions[0], wantComps[0]) {
		t.Errorf("SVC01 components = %v, want %v", svc.Elements[0].Repetitions, wantComps)
	}

	isa2 := events[9].Segment
	if got := isa2.Elements[5].Value; got != "SE~NDER        " {
		t.Errorf("ISA2 ISA06 = %q, want %q (old terminator kept as data)", got, "SE~NDER        ")
	}
	if got := isa2.Elements[12].Value; got != "000000777" {
		t.Errorf("ISA2 ISA13 = %q, want %q", got, "000000777")
	}
	if got := isa2.Offset; got != offsets[9] {
		t.Errorf("ISA2 offset = %d, want %d", got, offsets[9])
	}
}

// TestChunkingInvariance feeds the same stream with every possible single
// cut point, byte-by-byte, and with random chunkings; the event sequence
// must be identical to the single-Feed reference every time.
func TestChunkingInvariance(t *testing.T) {
	stream, _ := buildTwoInterchanges()
	data := []byte(stream)
	reference := runChunks(data, len(data))

	for cut := 0; cut <= len(data); cut++ {
		got := runChunks(data, cut)
		assertSameEvents(t, reference, got, "single cut")
	}

	got := runChunks(data, 1)
	assertSameEvents(t, reference, got, "byte-by-byte")

	for seed := int64(1); seed <= 20; seed++ {
		r := rand.New(rand.NewSource(seed))
		var sizes []int
		for n := len(data); n > 0; {
			sz := 1 + r.Intn(37)
			if sz > n {
				sz = n
			}
			sizes = append(sizes, sz)
			n -= sz
		}
		got := runChunks(data, sizes...)
		assertSameEvents(t, reference, got, "random chunking")
	}
}

// TestRecoveryNewISAContainsOldTerm drops IEA of the first interchange;
// the following ISA (which contains the old terminator as data) must
// recover framing with its own delimiters.
func TestRecoveryNewISAContainsOldTerm(t *testing.T) {
	var b streamBuilder
	b.seg(isaSegment('*', '^', ':', '~', "SENDER", "RECEIVER", "000000001"))
	b.seg("GS*HC*SEND*RECV*20251003*1200*1*X*005010X222~")
	b.seg("ST*837*0001~")
	b.seg("SE*2*0001~")
	b.seg("GE*1*1~")
	// no IEA; the next interchange starts right away and its ISA06
	// contains the previous terminator '~' as data.
	b.seg(isaSegment('|', '?', '%', '!', "SE~NDER", "RECV2", "000000777"))
	b.seg("GS|HC|SEND|RECV|20251003|1300|7|X|005010X222!")
	b.seg("ST|837|0002!")
	b.seg("NM1|IL|1|DOE!")
	b.seg("SE|3|0002!")
	b.seg("GE|1|7!")
	b.seg("IEA|1|000000777!")
	stream, offsets := b.String(), b.offsets

	events := runChunks([]byte(stream))
	wantKinds := []string{
		"S:ISA", "S:GS", "S:ST", "S:SE", "S:GE",
		"E:" + ErrMissingIEA,
		"S:ISA", "S:GS", "S:ST", "S:NM1", "S:SE", "S:GE", "S:IEA",
	}
	if got := eventKinds(events); !stringSliceEqual(got, wantKinds) {
		t.Fatalf("event kinds = %v, want %v", got, wantKinds)
	}
	if got := events[5].Error.Offset; got != offsets[5] {
		t.Errorf("MISSING_IEA offset = %d, want %d (new ISA start)", got, offsets[5])
	}
	isa2 := events[6].Segment
	if got := isa2.Offset; got != offsets[5] {
		t.Errorf("ISA2 offset = %d, want %d", got, offsets[5])
	}
	if got := isa2.Elements[5].Value; got != "SE~NDER        " {
		t.Errorf("ISA2 ISA06 = %q, want %q", got, "SE~NDER        ")
	}
	if got := isa2.Elements[12].Value; got != "000000777" {
		t.Errorf("ISA2 ISA13 = %q, want %q", got, "000000777")
	}
}
