package ws

import (
	"testing"
	"time"
)

func TestHubStopClosesClientsAndReturns(t *testing.T) {
	h := NewHub()
	client := newReadyTestClient(7, "Nazar")
	registerTestClient(h, client)

	runDone := make(chan struct{})
	go func() {
		h.Run()
		close(runDone)
	}()

	h.Stop()

	select {
	case <-h.Done():
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for hub shutdown")
	}

	select {
	case <-runDone:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for hub run loop to exit")
	}

	select {
	case <-client.done:
	default:
		t.Fatal("expected client to be closed during hub shutdown")
	}

	select {
	case _, ok := <-client.send:
		if ok {
			t.Fatal("expected client send channel to be closed")
		}
	default:
		t.Fatal("expected closed client send channel to be readable")
	}
}
