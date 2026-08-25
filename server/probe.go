package main

import "encoding/binary"

// Shared by the mature iPhone transport and the Android transport. 0xff is
// outside WireGuard's valid message type range (1..4), so old servers can
// forward this packet harmlessly while probe-aware servers echo it.
var probePingMagic = [4]byte{0xff, 'P', 'N', 'G'}

const probePacketLen = len(probePingMagic) + 8

func parseProbePacket(packet []byte) (uint64, bool) {
	if len(packet) != probePacketLen {
		return 0, false
	}
	for i, expected := range probePingMagic {
		if packet[i] != expected {
			return 0, false
		}
	}
	return binary.BigEndian.Uint64(packet[len(probePingMagic):]), true
}
