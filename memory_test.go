package x12val

import (
	"bytes"
	"runtime"
	"testing"
)

// TestMemoryConstant streams over one million short segments through the
// validator in small chunks. The emit callback only counts. Live heap must
// not grow with the number of processed segments.
func TestMemoryConstant(t *testing.T) {
	const bodySegments = 1_000_000
	var segCount, errCount int
	v := NewValidator(func(ev Event) {
		if ev.Kind == EventSegment {
			segCount++
		} else {
			errCount++
		}
	})
	feed := func(b []byte) {
		const chunk = 4096
		for len(b) > 0 {
			n := chunk
			if n > len(b) {
				n = len(b)
			}
			v.Feed(b[:n])
			b = b[n:]
		}
	}
	var head []byte
	head = append(head, buildISA('*', '^', ':', '~', "SENDER         ", "000000001")...)
	head = append(head, "GS*HC*SENDER*RECEIVER*20230101*1200*7*X*005010~"...)
	head = append(head, "ST*837*0001~"...)
	feed(head)
	// Stream body segments from a small reusable scratch buffer.
	body := []byte("NM1*1~")
	scratch := bytes.Repeat(body, 512) // 3 KiB
	fed := 0
	warmup := 100_000
	var heapAfterWarmup uint64
	for fed < bodySegments {
		n := len(scratch) / len(body)
		if rem := bodySegments - fed; rem < n {
			n = rem
		}
		v.Feed(scratch[:n*len(body)])
		fed += n
		if fed >= warmup && heapAfterWarmup == 0 {
			runtime.GC()
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			heapAfterWarmup = m.HeapAlloc
		}
	}
	// Correct tail: ST + 1M body + SE = 1000002 segments in the transaction.
	var tail []byte
	tail = append(tail, "SE*1000002*0001~"...)
	tail = append(tail, "GE*1*7~"...)
	tail = append(tail, "IEA*1*000000001~"...)
	feed(tail)
	v.Finish()
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	heapAfterAll := m.HeapAlloc
	if errCount != 0 {
		t.Fatalf("got %d error events, want 0", errCount)
	}
	// ISA, GS, ST, 1M body, SE, GE, IEA.
	if want := bodySegments + 6; segCount != want {
		t.Fatalf("got %d segment events, want %d", segCount, want)
	}
	const slack = 4 << 20 // 4 MiB
	if heapAfterAll > heapAfterWarmup+slack {
		t.Fatalf("live heap grew from %d to %d bytes across %d segments",
			heapAfterWarmup, heapAfterAll, bodySegments-warmup)
	}
	t.Logf("heap after warmup: %d, after %d segments: %d",
		heapAfterWarmup, bodySegments, heapAfterAll)
}
