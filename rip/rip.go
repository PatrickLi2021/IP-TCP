package rip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"ip-ip-pa/lnxconfig"
	protocol "ip-ip-pa/pkg"
	"net"
	"net/netip"
	"sync"
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
	Address          netip.Addr // final destination of route
	InitialTimestamp time.Time  // time when this route was inserted (to be used for updating purposes)
}

type RipInstance struct {
	neighborRouters []netip.Addr
	mutex           sync.Mutex
}

func (ripInstance *RipInstance) Initialize(configInfo lnxconfig.IPConfig) {
	ripInstance.neighborRouters = configInfo.RipNeighbors
}

func (ripInstance *RipInstance) sendRipRequest(stack *protocol.IPStack) {
	for destAddrPort := range ripInstance.neighborRouters {
		// Create RIP request
		ripPacket := RIPPacket{
			Command:     1,
			Num_entries: 0,
			Entries:     []RIPEntry{},
		}
		ripBytes, err := MarshalRIP(&ripPacket)
		if err != nil {
			fmt.Println("Error marshaling RIP packet message")
			return
		}

		// Convert neighbor destAddrPort (netip.AddrPort) to net.UDPAddr
		udpAddr := &net.UDPAddr{
			IP:   net.IP(destAddrPort.Addr().AsSlice()),
			Port: int(destAddrPort.Port()),
		}

		// Send RIP request bytes
		iface.Conn.WriteToUDP(ripBytes, udpAddr)
	}
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
