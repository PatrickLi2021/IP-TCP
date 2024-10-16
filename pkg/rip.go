package protocol

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"time"
)

const INF = 16
const ROUTE_TIME = 12 // routing refresh time
const ENTRY_TIME = 5  // periodic update time

type RIPPacket struct {
	Command     uint16
	Num_entries uint16
	Entries     []RIPEntry
}

type RIPEntry struct {
	Cost    uint32
	Address uint32
	Mask    uint32
}

type RouteEntry struct {
	Cost             int        // total cost of route
	Address          netip.Addr // source destination of route
	InitialTimestamp time.Time  // time when this route was inserted (to be used for updating purposes)
}

func MarshalRIP(ripPacket *RIPPacket) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, ripPacket.Command)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.BigEndian, ripPacket.Num_entries)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.BigEndian, ripPacket.Entries)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func UnmarshalRIP(payload []byte) (*RIPPacket, error) {
	// Extract metadata
	command := binary.BigEndian.Uint16(payload[0:2])
	numEntries := binary.BigEndian.Uint16(payload[2:4])

	// Create empty RIP packet struct
	packet := &RIPPacket{
		Command:     command,
		Num_entries: numEntries,
		Entries:     make([]RIPEntry, numEntries),
	}
	offset := 4
	for i := 0; i < int(numEntries); i++ {
		entry := RIPEntry{
			Cost:    binary.BigEndian.Uint32(payload[offset : offset+4]),
			Address: binary.BigEndian.Uint32(payload[offset+4 : offset+8]),
			Mask:    binary.BigEndian.Uint32(payload[offset+8 : offset+12]),
		}
		packet.Entries[i] = entry
		offset += 12
	}
	return packet, nil
}
