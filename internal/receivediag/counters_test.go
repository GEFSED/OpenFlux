package receivediag

import (
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentCountersAndClosedLogSchema(t *testing.T) {
	var c Counters
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				c.Add(AESPackets, 1)
				c.Fail(AESHeader)
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	if s := c.Snapshot(); s.Values[AESPackets] != 8000 || s.First != AESHeader {
		t.Fatal("lost concurrent counter/failure")
	}
	c.Fail(Legacy)
	if c.Snapshot().First != AESHeader {
		t.Fatal("first failure overwritten")
	}
	fields := strings.Fields(c.Line())
	if len(fields) != len(Names)+1 {
		t.Fatal("unexpected log fields")
	}
	for i, field := range fields[:len(Names)] {
		pair := strings.SplitN(field, "=", 2)
		if len(pair) != 2 || pair[0] != Names[i] {
			t.Fatal("unexpected counter name")
		}
		if _, err := strconv.ParseUint(pair[1], 10, 64); err != nil {
			t.Fatal("counter contains nonnumeric data")
		}
	}
	if fields[len(Names)] != "first_failure_layer=aes_header" {
		t.Fatal("unexpected enum")
	}
}
