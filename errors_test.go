package x12val

import (
	"testing"
)

// TestMixedErrorsAndRecovery builds one interchange containing a wrong SE
// count, an SE control number differing only by a leading zero, a missing
// SE, a missing GE and a wrong IEA count, followed by a fully legal
// interchange. Errors must not swallow the trailing interchange.
func TestMixedErrorsAndRecovery(t *testing.T) {
	var b streamBuilder
	b.seg(isaSegment('*', '^', ':', '~', "SENDER", "RECEIVER", "000000042"))
	b.seg("GS*HC*S*R*20251003*1200*9*X*005010X222~")
	b.seg("ST*837*0001~")
	b.seg("AAA*1~")
	b.seg("BBB*2~")
	// actual ST..SE count is 4; SE01=9 is wrong, SE02="00001" differs
	// from ST02="0001" only by a leading zero (byte-wise mismatch).
	b.seg("SE*9*00001~")
	b.seg("ST*837*0002~")
	b.seg("CCC*3~")
	// no SE for ST 0002, no GE for the group; IEA01=3 but only 1 GS.
	b.seg("IEA*3*000000042~")
	b.raw("\r\n")
	// Legal interchange with different delimiters must parse cleanly.
	b.seg(isaSegment('|', '?', '%', '!', "S2", "R2", "000000100"))
	b.seg("GS|HC|S|R|20251003|1300|5|X|005010X222!")
	b.seg("ST|837|0007!")
	b.seg("DDD|4!")
	b.seg("SE|3|0007!")
	b.seg("GE|1|5!")
	b.seg("IEA|1|000000100!")
	stream, offsets := b.String(), b.offsets

	events := runChunks([]byte(stream))
	wantKinds := []string{
		"S:ISA", "S:GS", "S:ST", "S:AAA", "S:BBB",
		"S:SE",
		"E:" + ErrSECount,
		"E:" + ErrSEControl,
		"S:ST", "S:CCC",
		"E:" + ErrMissingSE,
		"E:" + ErrMissingGE,
		"S:IEA",
		"E:" + ErrIEACount,
		"S:ISA", "S:GS", "S:ST", "S:DDD", "S:SE", "S:GE", "S:IEA",
	}
	if got := eventKinds(events); !stringSliceEqual(got, wantKinds) {
		t.Fatalf("event kinds =\n%v\nwant\n%v", got, wantKinds)
	}

	// Hand-determined error details, offsets point at the triggering
	// segment start.
	seOffset := offsets[5]
	ieaOffset := offsets[8]
	wantErrors := []Error{
		{Code: ErrSECount, Offset: seOffset, Detail: `SE01="9" actual=4`},
		{Code: ErrSEControl, Offset: seOffset, Detail: `SE02="00001" ST02="0001"`},
		{Code: ErrMissingSE, Offset: ieaOffset, Detail: "ST envelope closed by IEA"},
		{Code: ErrMissingGE, Offset: ieaOffset, Detail: "GS envelope closed by IEA"},
		{Code: ErrIEACount, Offset: ieaOffset, Detail: `IEA01="3" actual=1`},
	}
	var gotErrors []Error
	for _, e := range events {
		if e.Error != nil {
			gotErrors = append(gotErrors, *e.Error)
		}
	}
	if len(gotErrors) != len(wantErrors) {
		t.Fatalf("got %d errors, want %d", len(gotErrors), len(wantErrors))
	}
	for i, w := range wantErrors {
		if gotErrors[i] != w {
			t.Errorf("error %d = %+v, want %+v", i, gotErrors[i], w)
		}
	}

	// The trailing legal interchange is fully framed with fresh state:
	// its ISA13 is its own, not inherited from the broken interchange.
	isa2 := events[14].Segment
	if got := isa2.Elements[12].Value; got != "000000100" {
		t.Errorf("ISA2 ISA13 = %q, want %q", got, "000000100")
	}
	if got := isa2.Offset; got != offsets[9] {
		t.Errorf("ISA2 offset = %d, want %d", got, offsets[9])
	}
}

// TestEOFMidISA: input ends inside the ISA fixed block.
func TestEOFMidISA(t *testing.T) {
	partial := isaSegment('*', '^', ':', '~', "S", "R", "000000001")[:50]
	events := runChunks([]byte(partial))
	if len(events) != 1 || events[0].Error == nil {
		t.Fatalf("events = %v, want a single error", eventKinds(events))
	}
	want := Error{Code: ErrTruncatedSegment, Offset: 50, Detail: "input ended mid-segment"}
	if *events[0].Error != want {
		t.Errorf("error = %+v, want %+v", *events[0].Error, want)
	}
}

// TestEOFMidSegment: input ends inside a normal segment with all
// envelopes still open.
func TestEOFMidSegment(t *testing.T) {
	var b streamBuilder
	b.seg(isaSegment('*', '^', ':', '~', "SENDER", "RECEIVER", "000000001"))
	b.seg("GS*HC*S*R*20251003*1200*9*X*005010X222~")
	b.seg("ST*837*0001~")
	stream := b.String() + "NM1*IL"
	total := int64(len(stream))

	events := runChunks([]byte(stream))
	wantKinds := []string{
		"S:ISA", "S:GS", "S:ST",
		"E:" + ErrTruncatedSegment,
		"E:" + ErrMissingSE,
		"E:" + ErrMissingGE,
		"E:" + ErrMissingIEA,
	}
	if got := eventKinds(events); !stringSliceEqual(got, wantKinds) {
		t.Fatalf("event kinds = %v, want %v", got, wantKinds)
	}
	for i, code := range []string{ErrTruncatedSegment, ErrMissingSE, ErrMissingGE, ErrMissingIEA} {
		e := events[3+i].Error
		if e.Offset != total {
			t.Errorf("%s offset = %d, want total input length %d", code, e.Offset, total)
		}
	}
}
