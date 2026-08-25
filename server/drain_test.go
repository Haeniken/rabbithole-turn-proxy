package main

import (
	"sync"
	"testing"
)

func TestConnectionGateStopsAdmissionBeforeWait(t *testing.T) {
	var gate connectionGate
	if !gate.admit() || !gate.admit() {
		t.Fatal("gate refused a session before drain")
	}
	if active := gate.beginDrain(); active != 2 {
		t.Fatalf("active at drain = %d, want 2", active)
	}
	if gate.admit() {
		t.Fatal("gate admitted a new session while draining")
	}

	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		gate.done()
		gate.done()
		gate.wait()
	}()
	done.Wait()
	if active := gate.activeCount(); active != 0 {
		t.Fatalf("active after drain = %d, want 0", active)
	}
}
