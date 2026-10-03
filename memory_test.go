package x12val

import (
	"fmt"
	"runtime"
	"testing"
)

// TestMemoryStable feeds over one million short segments in small chunks
// while emit only counts. The result must be exact and live heap must not
// grow linearly with the number of processed segments.
func TestMemoryStable(t *testing.T) {
	const n = 1_000_000

	var segs, errs int64
	v := NewValidator(func(e Event) {
		if e.Segment != nil {
			segs++
		} else {
			errs++
		}
	})

	feed := func(s string) {
		v.Feed([]byte(s))
	}

	// Envelope: one ISA, one GS, one ST, n data segments, then trailers.
	feed(isaSegment('*', '^', ':', '~', "SENDER", "RECEIVER", "000000001"))
	feed("GS*HC*S*R*20251003*1200*9*X*005010X222~")
	feed("ST*837*0001~")

	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	// Generate data segments in small chunks without retaining them.
	const dataSeg = "X*1~"
	buf := make([]byte, 0, 4096)
	for i := 0; i < n; i++ {
		buf = append(buf, dataSeg...)
		if len(buf) >= 4096-len(dataSeg) {
			v.Feed(buf)
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		v.Feed(buf)
	}

	// ST..SE inclusive: ST + n data + SE.
	feed(fmt.Sprintf("SE*%d*0001~", n+2))
	feed("GE*1*9~")
	feed("IEA*1*000000001~")
	v.Finish()

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	if want := int64(n) + 6; segs != want {
		t.Errorf("segments = %d, want %d", segs, want)
	}
	if errs != 0 {
		t.Errorf("errors = %d, want 0", errs)
	}

	growth := int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	const limit = 32 << 20 // 32 MiB; retaining 1M segments would cost far more
	if growth > limit {
		t.Errorf("live heap grew by %d bytes over %d segments, want <= %d", growth, n, limit)
	}
}
