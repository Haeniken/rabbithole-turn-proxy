package main

import (
	"testing"
	"time"
)

func validSessionPreface(streamID byte) []byte {
	packet := make([]byte, udpSessionPrefaceLen)
	copy(packet, []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0x4d, 0xef, 0x80, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd})
	packet[16] = streamID
	return packet
}

func TestParseUDPSessionPreface(t *testing.T) {
	hello, ok := parseUDPSessionPreface(validSessionPreface(15))
	if !ok || hello.streamID != 15 || hello.id[0] != 0x12 || hello.supportsReorder {
		t.Fatalf("parseUDPSessionPreface()=(%v,%t), want stream 15", hello, ok)
	}
	capable, ok := parseUDPSessionPreface(validSessionPreface(15 | udpSessionReorderFlag))
	if !ok || capable.streamID != 15 || !capable.supportsReorder {
		t.Fatalf("capable preface=(%v,%t), want stream 15 with reorder", capable, ok)
	}
}

func TestParseUDPSessionPrefaceKeepsLegacyTrafficOnLegacyPath(t *testing.T) {
	tests := [][]byte{
		{1, 0, 0, 0},
		make([]byte, udpSessionPrefaceLen),
		validSessionPreface(udpSessionMaxStreams),
		append(validSessionPreface(1), 0),
	}
	for _, packet := range tests {
		if _, ok := parseUDPSessionPreface(packet); ok {
			t.Fatalf("legacy packet of length %d accepted as a session preface", len(packet))
		}
	}
}

func testSessionLane(id byte, depth int) *udpSessionLane {
	lane := &udpSessionLane{streamID: id, supportsReorder: true, out: make(chan *udpSessionPacket, udpSessionQueueSize)}
	for i := 0; i < depth; i++ {
		lane.out <- &udpSessionPacket{}
	}
	return lane
}

func TestUDPLaneSelectorPinsLegacyClient(t *testing.T) {
	var lanes [udpSessionMaxStreams]*udpSessionLane
	lanes[0] = testSessionLane(0, 0)
	lanes[1] = testSessionLane(1, 0)
	lanes[0].supportsReorder = false
	lanes[1].supportsReorder = false
	selector := udpLaneSelector{}
	now := time.Unix(1, 0)
	for i := 0; i < udpSessionStripeSize*2; i++ {
		if got := selector.choose(lanes, now.Add(time.Duration(i)*time.Second)); got != lanes[0] {
			t.Fatalf("legacy packet %d selected stream %v, want pinned stream 0", i, got)
		}
	}
}

func TestUDPLaneSelectorKeepsStripe(t *testing.T) {
	var lanes [udpSessionMaxStreams]*udpSessionLane
	lanes[0] = testSessionLane(0, 0)
	lanes[1] = testSessionLane(1, 0)
	selector := udpLaneSelector{}
	now := time.Unix(1, 0)
	for i := 0; i < udpSessionStripeSize; i++ {
		if got := selector.choose(lanes, now.Add(time.Duration(i)*time.Microsecond)); got != lanes[0] {
			t.Fatalf("packet %d selected stream %v, want 0", i, got)
		}
	}
	if got := selector.choose(lanes, now.Add(udpSessionStripeSize*time.Microsecond)); got != lanes[1] {
		t.Fatalf("next stripe selected %v, want stream 1", got)
	}
}

func TestUDPLaneSelectorStartsNewFlowletOnNextLane(t *testing.T) {
	var lanes [udpSessionMaxStreams]*udpSessionLane
	lanes[0] = testSessionLane(0, 0)
	lanes[1] = testSessionLane(1, 0)
	selector := udpLaneSelector{}
	now := time.Unix(1, 0)
	if got := selector.choose(lanes, now); got != lanes[0] {
		t.Fatalf("first flowlet selected %v, want stream 0", got)
	}
	if got := selector.choose(lanes, now.Add(udpSessionFlowletGap+time.Millisecond)); got != lanes[1] {
		t.Fatalf("second flowlet selected %v, want stream 1", got)
	}
}

func TestUDPLaneSelectorEscapesQueuedLane(t *testing.T) {
	var lanes [udpSessionMaxStreams]*udpSessionLane
	lanes[0] = testSessionLane(0, udpSessionStripeSize)
	lanes[1] = testSessionLane(1, 0)
	selector := udpLaneSelector{current: 0, hasCurrent: true, remaining: udpSessionStripeSize - 1}
	if got := selector.choose(lanes, time.Unix(1, 0)); got != lanes[1] {
		t.Fatalf("selector stayed on queued stream: got %v, want stream 1", got)
	}
}
