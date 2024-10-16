package protocol

import (
	"bytes"
	"fmt"
	"ip-ip-pa/lnxconfig"
	"net"
	"net/netip"
	"sync"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

const MAX_PACKET_SIZE = 1400

type IPPacket struct {
	Header  ipv4header.IPv4Header
	Payload []byte
}

type Interface struct {
	Name      string                        // the name of the interface
	IP        netip.Addr                    // the IP address of the interface on this host
	Prefix    netip.Prefix                  // the network submask/prefix
	Neighbors map[netip.Addr]netip.AddrPort // maps (virtual) IPs to UDP port
	Udp       *net.UDPAddr                  // the UDP address of the interface on this host
	Down      bool                          // whether the interface is down or not
	Conn      *net.UDPConn                  // listen to incoming UDP packets
	Mutex     sync.RWMutex
}

type ipCostInterfaceTuple struct {
	NextHopIP   netip.Addr
	Cost        uint32
	Interface   *Interface
	Type        string
	LastRefresh time.Time
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType     lnxconfig.RoutingMode
	Forward_table   map[netip.Prefix]*ipCostInterfaceTuple // maps IP prefixes to ip-cost-interface tuple
	Handler_table   map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces      map[netip.Addr]*Interface              // maps interface names to interfaces
	Mutex           sync.RWMutex                           // for concurrency
	NameToInterface map[string]*Interface                  // maps name to interface for REPL commands
	RipNeighbors    []netip.Addr
}

func (stack *IPStack) Initialize(configInfo lnxconfig.IPConfig) error {
	// Populate fields of stack
	stack.RoutingType = configInfo.RoutingMode
	stack.Forward_table = make(map[netip.Prefix]*ipCostInterfaceTuple)
	stack.Handler_table = make(map[uint16]HandlerFunc)
	stack.Interfaces = make(map[netip.Addr]*Interface)
	stack.NameToInterface = make(map[string]*Interface)
	stack.Mutex = sync.RWMutex{}

	// create interfaces to populate map of interfaces for IPStack struct
	for _, lnxInterface := range configInfo.Interfaces {
		// one new interface
		newInterface := Interface{
			Name:   lnxInterface.Name,
			IP:     lnxInterface.AssignedIP,
			Prefix: lnxInterface.AssignedPrefix,
			Down:   false,
			Mutex:  sync.RWMutex{},
		}
		// Creating UDP conn for each interface
		serverAddr, err := net.ResolveUDPAddr("udp4", lnxInterface.UDPAddr.String())
		if err != nil {
			fmt.Println(err)
			return err
		}
		conn, err := net.ListenUDP("udp4", serverAddr)
		if err != nil {
			fmt.Println(err)
			return err
		}
		// add udp addr and conn to interface struct
		newInterface.Udp = serverAddr
		newInterface.Conn = conn

		// Map interface name and ip to interface struct for ip stack struct
		stack.Interfaces[newInterface.IP] = &newInterface
		stack.NameToInterface[newInterface.Name] = &newInterface

		// new neighbors map for new interface
		newInterface.Neighbors = make(map[netip.Addr]netip.AddrPort)
	}

	// for all the node's neighbors, loop through and add to correct interfaces map
	for _, neighbor := range configInfo.Neighbors {
		// This is saying "I can reach this neighbor through this interface lnxInterface"
		interface_struct := stack.NameToInterface[neighbor.InterfaceName]
		interface_struct.Neighbors[neighbor.DestAddr] = neighbor.UDPAddr
	}

	// Populate forwarding table with interfaces ON THIS NODE
	for _, iface := range stack.Interfaces {
		stack.Forward_table[iface.Prefix] = &ipCostInterfaceTuple{
			NextHopIP: iface.IP,
			Cost:      0,
			Interface: iface,
			Type:      "L",
		}
	}

	// Add rip neighbors only if routing type IS RIP
	if stack.RoutingType == 2 {
		stack.RipNeighbors = configInfo.RipNeighbors
	}

	// Add default route entry only if routing type is NOT RIP (aka NOT 2)
	if stack.RoutingType != 2 {
		for prefix, address := range configInfo.StaticRoutes {
			stack.Forward_table[prefix] = &ipCostInterfaceTuple{
				NextHopIP:   address,
				Cost:        0, 
				Interface:   nil,
				Type:        "S",
			}
		}
	}
	return nil
}

func (stack *IPStack) SendIP(originalSrc *netip.Addr, TTL int, dest netip.Addr, protocolNum uint16, data []byte) error {
	// check if sending to itself
	for ip := range stack.Interfaces {
		if ip == dest {
			if (stack.Interfaces[ip].Down) {
				// interface is down, don't call callback
				return nil
			}
			// sending to itself, so call callback
			packet := &IPPacket{
				Header: ipv4header.IPv4Header{
					Version:  4,
					Len:      20, // Header length is always 20 when no IP options
					TOS:      0,
					TotalLen: ipv4header.HeaderLen + len(data),
					ID:       0,
					Flags:    0,
					FragOff:  0,
					TTL:      TTL,
					Protocol: int(protocolNum),
					Checksum: 0, // Should be 0 until checksum is computed
					Src:      dest,
					Dst:      dest,
					Options:  []byte{},
				},
				Payload: data,
			}
			stack.Handler_table[protocolNum](packet)
			return nil
		}
	}
	// Find longest prefix match
	srcIP, destAddrPort := stack.findPrefixMatch(dest)
	if destAddrPort == nil {
		// no match found, drop packet
		fmt.Println("A")
		return nil
	}
	iface, exists := stack.Interfaces[*srcIP]
	if exists && iface.Down {
		// drop packet, if src is down
		fmt.Println("down")
		return nil
	}
	if protocolNum == 0 {
		fmt.Println("in send ip, dest = ")
		fmt.Println(destAddrPort)
	}
	if originalSrc == nil {
		originalSrc = srcIP
	}

	// Construct IP packet header
	header := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(data),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      TTL,
		Protocol: int(protocolNum),
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      *originalSrc,
		Dst:      dest,
		Options:  []byte{},
	}

	headerBytes, err := header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}

	// Compute header checksum
	header.Checksum = int(ComputeChecksum(headerBytes))
	headerBytes, err = header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}

	// Construct all bytes of the IP packet
	bytesToSend := make([]byte, 0, len(headerBytes)+len(data))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, []byte(data)...)
	bytesToSend = bytes.TrimRight(bytesToSend, "\x00")

	// TODO
	bytesWritten, err := (stack.Interfaces[*srcIP].Conn).WriteToUDP(bytesToSend, destAddrPort)
	if err != nil {
		fmt.Println(err)
		return err
	}
	if protocolNum == 0 {
		fmt.Printf("Sent %d bytes\n", bytesWritten)
	}
	return nil
}

func ComputeChecksum(headerBytes []byte) uint16 {
	checksum := header.Checksum(headerBytes, 0)
	checksumInv := checksum ^ 0xffff
	return checksumInv
}

func (stack *IPStack) findPrefixMatch(addr netip.Addr) (*netip.Addr, *net.UDPAddr) {
	// searching for longest prefix match
	var longestMatch netip.Prefix
	var bestTuple *ipCostInterfaceTuple = nil
	stack.Mutex.RLock()
	for pref, tuple := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				bestTuple = tuple
			}
		}
	}
	stack.Mutex.RUnlock()

	// no match found, drop packet
	if bestTuple == nil {
		return nil, nil
	}

	if bestTuple.Type != "L" {
		// hit route case, make exactly one additional lookup in the table (we are guaranteed to make at most 2 calls
		// here)
		return stack.findPrefixMatch(bestTuple.NextHopIP)
	}

	// // have not hit a default catch all, so check interface first to see if it matches dest
	// if (bestTuple.Interface.IP == addr) {
	// 	udpAddr := stack.Interfaces[bestTuple.Interface.IP].Udp
	// 	return nil, udpAddr // would use same src as interface that received the packet
	// }
	// otherwise, loop thru all of interfaces neighbors
	for ip, port := range bestTuple.Interface.Neighbors {
		if ip == addr {
			// Convert netip.AddrPort to *net.UDPAddr
			udpAddr := &net.UDPAddr{
				IP:   net.IP(port.Addr().AsSlice()),
				Port: int(port.Port()),
			}
			return &bestTuple.Interface.IP, udpAddr
		}
	}

	// no match found in neighbors
	// should never reach here
	return nil, nil
}

func (stack *IPStack) Receive(iface *Interface) error {
	buffer := make([]byte, MAX_PACKET_SIZE)
	_, _, err := iface.Conn.ReadFromUDP(buffer)
	if iface.Down {
		// if down can't receive
		return nil
	}

	if err != nil {
		fmt.Println(err)
		return err
	}
	hdr, err := ipv4header.ParseHeader(buffer)
	if err != nil {
		fmt.Println("Error parsing header", err)
		return nil 
	}

	if hdr.Protocol == 0 {
		fmt.Println("in receieve")
	}
	hdrSize := hdr.Len

	// validate checksum
	hdrBytes := buffer[:hdrSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := header.Checksum(hdrBytes, checksumFromHeader)
	if computedChecksum != checksumFromHeader {
		// if checksum invalid, drop packet and return
		return nil
	}
	// if TTL < 0, drop packet
	if hdr.TTL <= 0 {
		return nil
	}

	// packet payload
	message := buffer[hdrSize:]

	// check packet's dest ip
	correctDest := hdr.Dst == iface.IP
	for interIP := range stack.Interfaces {
		if interIP == hdr.Dst {
			correctDest = true
			break
		}
	}
	if correctDest {
		// packet has reached destination

		// create packet to pass into callback
		packet := &IPPacket{
			Header:  *hdr,
			Payload: message,
		}
		// calling callback
		stack.Handler_table[uint16(hdr.Protocol)](packet)
	} else {
		// packet has NOT reached dest yet
		err := stack.SendIP(&hdr.Src, hdr.TTL-1, hdr.Dst, uint16(hdr.Protocol), message)
		return err
	}
	return nil
}

func (stack *IPStack) RegisterRecvHandler(protocolNum uint16, callbackFunc HandlerFunc) {
	stack.Handler_table[protocolNum] = callbackFunc
}
