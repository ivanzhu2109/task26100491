package x12val

import (
	"reflect"
	"testing"
)

// mixedErrorStream builds one interchange with a wrong SE count, a control
// number differing only by a leading zero, and missing SE and GE tails,
// followed by one fully valid interchange.
func mixedErrorStream() []byte {
	var b []byte
	// Interchange A: ISA control 000000042.
	b = append(b, buildISA('*', '^', ':', '~', "SENDER         ", "000000042")...)
	b = append(b, "GS*FA*SENDER*RECEIVER*20230101*1200*7*X*005010~"...)
	b = append(b, "ST*837*0001~"...)
	b = append(b, "NM1*IL*1*SMITH*JOHN~"...)
	b = append(b, "REF*EI*ABC~"...)
	b = append(b, "SE*9*00001~"...) // actual count 4; control has extra leading zero
	b = append(b, "ST*837*0002~"...)
	b = append(b, "NM1*QC*1*DOE*JANE~"...)
	// No SE for ST 0002, no GE for the group.
	b = append(b, "IEA*1*000000042~"...)
	// Interchange B: fully valid, must not inherit A's counters.
	b = append(b, buildISA('*', '^', ':', '~', "SENDER         ", "000000043")...)
	b = append(b, "GS*FA*SENDER*RECEIVER*20230101*1200*8*X*005010~"...)
	b = append(b, "ST*837*1001~"...)
	b = append(b, "NM1*IL*1*SMITH*JOHN~"...)
	b = append(b, "SE*3*1001~"...)
	b = append(b, "GE*1*8~"...)
	b = append(b, "IEA*1*000000043~"...)
	return b
}

// Hand-determined expected event sequence for mixedErrorStream.
type wantEvent struct {
	kind     EventKind
	tag      string
	offset   int64
	code     ErrorCode
	expected string
	actual   string
}

func seg(tag string, off int64) wantEvent {
	return wantEvent{kind: EventSegment, tag: tag, offset: off}
}
func errE(code ErrorCode, off int64, exp, act string) wantEvent {
	return wantEvent{kind: EventError, offset: off, code: code, expected: exp, actual: act}
}

var mixedErrorEvents = []wantEvent{
	seg("ISA", 0),
	seg("GS", 106),
	seg("ST", 153),
	seg("NM1", 165),
	seg("REF", 185),
	seg("SE", 196),
	errE(CodeSECountMismatch, 196, "9", "4"),
	errE(CodeSEControlMismatch, 196, "0001", "00001"),
	seg("ST", 207),
	seg("NM1", 219),
	errE(CodeMissingSE, 237, "", ""),
	errE(CodeMissingGE, 237, "", ""),
	seg("IEA", 237),
	seg("ISA", 253),
	seg("GS", 359),
	seg("ST", 406),
	seg("NM1", 418),
	seg("SE", 438),
	seg("GE", 448),
	seg("IEA", 455),
}

func checkEvents(t *testing.T, events []Event, want []wantEvent) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d\ngot: %v", len(events), len(want), events)
	}
	for i, w := range want {
		ev := events[i]
		if ev.Kind != w.kind {
			t.Fatalf("event %d kind = %v, want %v", i, ev.Kind, w.kind)
		}
		if w.kind == EventSegment {
			if ev.Segment.Tag != w.tag || ev.Segment.Offset != w.offset {
				t.Errorf("event %d = %s@%d, want %s@%d",
					i, ev.Segment.Tag, ev.Segment.Offset, w.tag, w.offset)
			}
			continue
		}
		e := ev.Error
		if e.Code != w.code || e.Offset != w.offset ||
			e.Expected != w.expected || e.Actual != w.actual {
			t.Errorf("event %d = %s@%d exp=%q act=%q, want %s@%d exp=%q act=%q",
				i, e.Code, e.Offset, e.Expected, e.Actual,
				w.code, w.offset, w.expected, w.actual)
		}
	}
}

func TestMixedErrorsHandChecked(t *testing.T) {
	input := mixedErrorStream()
	if len(input) != 471 {
		t.Fatalf("total input length = %d, want 471", len(input))
	}
	checkEvents(t, runWhole(t, input), mixedErrorEvents)
}

func TestMixedErrorsChunkingInvariance(t *testing.T) {
	input := mixedErrorStream()
	want := runWhole(t, input)
	bytewise := make([][]byte, len(input))
	for i := range input {
		bytewise[i] = input[i : i+1]
	}
	if got := runChunks(t, input, bytewise); !reflect.DeepEqual(got, want) {
		t.Fatal("bytewise event sequence differs from whole-input run")
	}
	for i := 0; i <= len(input); i++ {
		if got := runChunks(t, input, [][]byte{input[:i], input[i:]}); !reflect.DeepEqual(got, want) {
			t.Fatalf("cut at %d: event sequence differs", i)
		}
	}
}

func TestEOFMidISA(t *testing.T) {
	isa := buildISA('*', '^', ':', '~', "SENDER         ", "000000001")
	input := isa[:50]
	checkEvents(t, runWhole(t, input), []wantEvent{
		errE(CodeTruncatedISA, 50, "", ""),
	})
}

func TestEOFMidSegment(t *testing.T) {
	var input []byte
	input = append(input, buildISA('*', '^', ':', '~', "SENDER         ", "000000001")...)
	input = append(input, "GS*FA*SE"...)
	if len(input) != 114 {
		t.Fatalf("input length = %d, want 114", len(input))
	}
	checkEvents(t, runWhole(t, input), []wantEvent{
		seg("ISA", 0),
		errE(CodeTruncatedSegment, 114, "", ""),
		errE(CodeMissingIEA, 114, "", ""),
	})
}
