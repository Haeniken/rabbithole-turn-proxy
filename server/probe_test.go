package main

import (
	"encoding/binary"
	"testing"
)

func TestParseProbePacket(t *testing.T) {
	packet := make([]byte, probePacketLen)
	copy(packet, probePingMagic[:])
	binary.BigEndian.PutUint64(packet[len(probePingMagic):], 42)
	if seq, ok := parseProbePacket(packet); !ok || seq != 42 {
		t.Fatalf("parseProbePacket()=(%d,%t), want (42,true)", seq, ok)
	}
}

func TestParseProbePacketRejectsRegularTraffic(t *testing.T) {
	if _, ok := parseProbePacket([]byte{1, 0, 0, 0}); ok {
		t.Fatal("regular WireGuard traffic was accepted as a probe")
	}
}
