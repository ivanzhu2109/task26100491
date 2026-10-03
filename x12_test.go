package x12val

import (
	"math/rand"
	"reflect"
	"testing"
)

// buildISA builds a fixed 106-byte ISA segment. sender must be 15 bytes,
// ctrl must be 9 bytes.
func buildISA(elem, rep, comp, term byte, sender, ctrl string) []byte {
	fields := []string{
		"00", "          ", "00", "          ",
		"ZZ", sender, "ZZ", "RECEIVER       ",
		"230101", "1200", string(rep), "00501", ctrl, "0", "T", string(comp),
	}
	widths := []int{2, 10, 2, 10, 2, 15, 2, 15, 6, 4, 1, 5, 9, 1, 1, 1}
	var b []byte
	b = append(b, "ISA"...)
	for i, f := range fields {
		if len(f) != widths[i] {
			panic("bad ISA field width")
		}
		b = append(b, elem)
		b = append(b, f...)
	}
	b = append(b, term)
	if len(b) != 106 {
		panic("bad ISA length")
	}
	return b
}

func runChunks(t *testing.T, input []byte, chunks [][]byte) []Event {
	t.Helper()
	var events []Event
	v := NewValidator(func(ev Event) { events = append(events, ev) })
	for _, c := range chunks {
		v.Feed(c)
	}
	v.Finish()
	return events
}

func runWhole(t *testing.T, input []byte) []Event {
	t.Helper()
	return runChunks(t, input, [][]byte{input})
}

// twoInterchanges returns a stream of two concatenated interchanges with
// different delimiters. The second ISA contains the first interchange's
// segment terminator '~' inside a fixed-width field. Data includes the
// text "ISA" as element content, a repeated element and a trailing empty
// component. Segments are separated by CR/LF in places.
func twoInterchanges() []byte {
	var b []byte
	b = append(b, buildISA('*', '^', ':', '~', "SENDER         ", "000000001")...)
	b = append(b, '\n')
	b = append(b, "GS*HC*SENDER*RECEIVER*20230101*1200*7*X*005010~\n"...)
	b = append(b, "ST*837*0001~\n"...)
	b = append(b, "NM1*IL*1*DOISA*JOHN~\n"...)
	b = append(b, "REF*A^B*X:~\n"...)
	b = append(b, "N4*ISA*VILLE~\n"...)
	b = append(b, "SE*5*0001~\n"...)
	b = append(b, "GE*1*7~\n"...)
	b = append(b, "IEA*1*000000001~\r\n"...)
	b = append(b, buildISA('|', '{', '}', '!', "SEND~ER        ", "000000002")...)
	b = append(b, "GS|HC|SENDER|RECEIVER|20230101|1200|9|X|005010!"...)
	b = append(b, "ST|837|0002!"...)
	b = append(b, "NM1|IL|1|DOE|JANE!"...)
	b = append(b, "SE|3|0002!"...)
	b = append(b, "GE|1|9!"...)
	b = append(b, "IEA|1|000000002!"...)
	return b
}

// Hand-computed expected segment offsets for twoInterchanges.
var twoInterchangeOffsets = []struct {
	tag    string
	offset int64
}{
	{"ISA", 0},
	{"GS", 107},
	{"ST", 155},
	{"NM1", 168},
	{"REF", 189},
	{"N4", 201},
	{"SE", 215},
	{"GE", 226},
	{"IEA", 234},
	{"ISA", 252},
	{"GS", 358},
	{"ST", 405},
	{"NM1", 417},
	{"SE", 435},
	{"GE", 445},
	{"IEA", 452},
}

func TestTwoInterchangesHandChecked(t *testing.T) {
	input := twoInterchanges()
	if len(input) != 468 {
		t.Fatalf("total input length = %d, want 468", len(input))
	}
	events := runWhole(t, input)

	var segs []SegmentEvent
	for _, ev := range events {
		if ev.Kind == EventError {
			t.Fatalf("unexpected error event: %+v", ev.Error)
		}
		segs = append(segs, *ev.Segment)
	}
	if len(segs) != len(twoInterchangeOffsets) {
		t.Fatalf("got %d segments, want %d", len(segs), len(twoInterchangeOffsets))
	}
	for i, want := range twoInterchangeOffsets {
		if segs[i].Tag != want.tag || segs[i].Offset != want.offset {
			t.Errorf("segment %d = %q@%d, want %q@%d",
				i, segs[i].Tag, segs[i].Offset, want.tag, want.offset)
		}
	}

	isa1 := segs[0]
	if len(isa1.Elements) != 16 {
		t.Fatalf("ISA has %d elements, want 16", len(isa1.Elements))
	}
	if isa1.Elements[11].Raw != "00501" || isa1.Elements[12].Raw != "000000001" {
		t.Errorf("ISA12/ISA13 = %q/%q", isa1.Elements[11].Raw, isa1.Elements[12].Raw)
	}
	if isa1.Elements[15].Raw != ":" || isa1.Elements[15].Repeats != nil {
		t.Errorf("ISA16 = %+v, want raw ':' with no repeats", isa1.Elements[15])
	}
	if isa1.Elements[1].Raw != "          " {
		t.Errorf("ISA02 spaces were trimmed: %q", isa1.Elements[1].Raw)
	}

	// The second ISA carries the first interchange's terminator '~' as data.
	isa2 := segs[9]
	if isa2.Elements[5].Raw != "SEND~ER        " {
		t.Errorf("ISA2 sender = %q, want %q", isa2.Elements[5].Raw, "SEND~ER        ")
	}
	if isa2.Elements[12].Raw != "000000002" {
		t.Errorf("ISA2 control = %q", isa2.Elements[12].Raw)
	}

	// Repeated element and trailing empty component are preserved.
	ref := segs[4]
	wantRef := []Element{
		{Raw: "A^B", Repeats: [][]string{{"A"}, {"B"}}},
		{Raw: "X:", Repeats: [][]string{{"X", ""}}},
	}
	if !reflect.DeepEqual(ref.Elements, wantRef) {
		t.Errorf("REF elements = %+v, want %+v", ref.Elements, wantRef)
	}

	// "ISA" as plain element text is not an interchange start.
	n4 := segs[5]
	if n4.Tag != "N4" || n4.Elements[0].Raw != "ISA" {
		t.Errorf("N4 = %q %+v", n4.Tag, n4.Elements)
	}
}

func TestChunkingInvariance(t *testing.T) {
	input := twoInterchanges()
	want := runWhole(t, input)

	check := func(name string, chunks [][]byte) {
		t.Helper()
		got := runChunks(t, input, chunks)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: event sequence differs from whole-input run", name)
		}
	}

	// Every single cut point.
	for i := 0; i <= len(input); i++ {
		check("cut", [][]byte{input[:i], input[i:]})
	}
	// Byte by byte.
	bytewise := make([][]byte, len(input))
	for i := range input {
		bytewise[i] = input[i : i+1]
	}
	check("bytewise", bytewise)
	// Random chunkings.
	for seed := int64(1); seed <= 5; seed++ {
		r := rand.New(rand.NewSource(seed))
		var chunks [][]byte
		for pos := 0; pos < len(input); {
			n := 1 + r.Intn(23)
			if pos+n > len(input) {
				n = len(input) - pos
			}
			chunks = append(chunks, input[pos:pos+n])
			pos += n
		}
		check("random", chunks)
	}
}
