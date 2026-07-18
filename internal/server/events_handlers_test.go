package server

import (
	"errors"
	"slices"
	"testing"
)

// A heartbeat carries the broker's global head seq, so it must not be written
// ahead of events still queued for this client. drainBufferedEvents is what the
// handler runs before each heartbeat to flush the queue first.
func TestDrainBufferedEventsWritesAllQueuedInOrderThenEmpties(t *testing.T) {
	t.Parallel()

	ch := make(chan event, 10)
	ch <- event{Seq: 5}
	ch <- event{Seq: 6}
	ch <- event{Seq: 7}

	var got []uint64
	open, err := drainBufferedEvents(ch, func(e event) error {
		got = append(got, e.Seq)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !open {
		t.Fatal("expected the channel to stay open")
	}
	if want := []uint64{5, 6, 7}; !slices.Equal(got, want) {
		t.Fatalf("drained %v, want %v", got, want)
	}
	// The queue is now empty, so a heartbeat emitted next carries a head seq the
	// client has already been sent up to — no leapfrog, no spurious resync.
	select {
	case e := <-ch:
		t.Fatalf("expected an empty channel after draining, still held seq %d", e.Seq)
	default:
	}
}

func TestDrainBufferedEventsStopsOnClosedChannel(t *testing.T) {
	t.Parallel()

	ch := make(chan event, 2)
	ch <- event{Seq: 1}
	close(ch)

	var got []uint64
	open, err := drainBufferedEvents(ch, func(e event) error {
		got = append(got, e.Seq)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if open {
		t.Fatal("expected open=false once the channel is closed")
	}
	if want := []uint64{1}; !slices.Equal(got, want) {
		t.Fatalf("drained %v, want %v", got, want)
	}
}

func TestDrainBufferedEventsStopsOnWriteError(t *testing.T) {
	t.Parallel()

	ch := make(chan event, 3)
	ch <- event{Seq: 1}
	ch <- event{Seq: 2}
	boom := errors.New("write failed")

	var got []uint64
	open, err := drainBufferedEvents(ch, func(e event) error {
		got = append(got, e.Seq)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the write error to propagate, got %v", err)
	}
	if !open {
		t.Fatal("a write error is not a closed channel; open should stay true")
	}
	if want := []uint64{1}; !slices.Equal(got, want) {
		t.Fatalf("expected draining to stop at the failing write, drained %v", got)
	}
}
