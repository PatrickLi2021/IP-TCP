package main

import (
	"fmt"
	"ip-ip-pa/lnxconfig"
	"net"
	"net/netip"
	"strconv"
	"sync"

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
	Udp       *net.UDPAddr                 // the UDP address of the interface on this host
	Down      bool                          // whether the interface is down or not
	Conn      *net.UDPConn                  // listen to incoming UDP packets
}

type ipCostInterfaceTuple struct {
	ip netip.Addr
	cost      uint16
	Interface *Interface
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType   lnxconfig.RoutingMode
	Forward_table map[netip.Prefix]*ipCostInterfaceTuple // maps IP prefixes to ip-cost-interface tuple
	Handler_table map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces    map[string]*Interface               // maps interface names to interfaces
	Mutex         sync.Mutex                          // for concurrency
}

func (stack *IPStack) Initialize(configInfo lnxconfig.IPConfig) error {
	// Populate fields of stack
	stack.RoutingType = configInfo.RoutingMode
	stack.Forward_table = make(map[netip.Prefix]*ipCostInterfaceTuple)
	stack.Handler_table = make(map[uint16]HandlerFunc)              
	stack.Interfaces = make(map[string]*Interface)               
	stack.Mutex = sync.Mutex{}

	// create interfaces to populate map of interfaces for IPStack struct
	for _, lnxInterface := range configInfo.Interfaces {
		// one new interface
		newInterface := Interface{
			Name:   lnxInterface.Name,
			IP:     lnxInterface.AssignedIP,
			Prefix: lnxInterface.AssignedPrefix,
			Down:   false,
		}

		// Creating UDP conn for each interface
		serverAddr, err := net.ResolveUDPAddr("udp4", newInterface.Udp.String())
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

		// Map interface name to interface struct for ip stack struct- TODO maybe change this map key
		stack.Interfaces[newInterface.Name] = &newInterface

		// new neighbors map for new interface
		newInterface.Neighbors = make(map[netip.Addr]netip.AddrPort)
	}

	// for all the node's neighbors, loop through and add to correct interfaces map:
	for _, neighbor := range configInfo.Neighbors {
		// This is saying "I can reach this neighbor through this interface lnxInterface"
		interface_struct := stack.Interfaces[neighbor.InterfaceName]
		interface_struct.Neighbors[neighbor.DestAddr] = neighbor.UDPAddr	
	}
	return nil
}

func (stack *IPStack) SendIP(src netip.Addr, prevTTL int, prevChecksum uint16, dest netip.Addr, protocolNum uint16, data []byte) error {
	// Construct IP packet header
	header := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(data),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      prevTTL-1,
		Protocol: int(protocolNum),
		Checksum: int(prevChecksum), // Should be 0 until checksum is computed
		Src:      src,
		Dst:      dest,
		Options:  []byte{},
	}
	headerBytes, err := header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}
	// Compute header checksum
	header.Checksum = int(ComputeChecksum(headerBytes, prevChecksum))
	headerBytes, err = header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}
	// Construct all bytes of the IP packet
	bytesToSend := make([]byte, 0, len(headerBytes)+len(data))
	bytesToSend = append(bytesToSend, headerBytes...)
	bytesToSend = append(bytesToSend, []byte(data)...)

	// Find longest prefix match
	destInterface := stack.findPrefixMatch(header.Dst).Interface

	bytesWritten, err := destInterface.Conn.WriteToUDP(bytesToSend, destInterface.Udp)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
	return nil
}

func ComputeChecksum(headerBytes []byte, prevChecksum uint16) uint16 {
	checksum := header.Checksum(headerBytes, prevChecksum)
	checksumInv := checksum ^ 0xffff
	return checksumInv
}

func (stack *IPStack) findPrefixMatch(addr netip.Addr) *ipCostInterfaceTuple {
	var longestMatch netip.Prefix // Changed to netip.Prefix instead of *netip.Prefix
	var bestTuple *ipCostInterfaceTuple

	for pref, tuple := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				bestTuple = tuple
			}
		}
	}

	// check tuple to see if we hit deafult case and need to re-look up ip in forwarding table
	if (bestTuple.Interface != nil) {
		return bestTuple
	} else {
		// hit default case, keep looking
		return stack.findPrefixMatch(bestTuple.ip)
	}
}



func (stack *IPStack) Receive(iface *Interface) error {
	buffer := make([]byte, MAX_PACKET_SIZE)
	_, _, err := iface.Conn.ReadFromUDP(buffer)
	if (err != nil) {
		fmt.Println(err)
		return err
	}

	hdr, err := ipv4header.ParseHeader(buffer)
	if (err != nil) {
		fmt.Println("Error parsing header", err)
		return nil // TODO? what to do? node should not crash or exit, instead just drop message and return to processing
	}

	hdrSize := hdr.Len

	// validate checksum
	hdrBytes := buffer[:hdrSize]
	checksumFromHeader := uint16(hdr.Checksum)
	computedChecksum := header.Checksum(hdrBytes, checksumFromHeader)
	if (computedChecksum != checksumFromHeader) {
		// if checksum invalid, drop packet and return
		fmt.Println("Error validating checksum in packet header")
		return nil
	}

	// packet payload
	message := buffer[hdrSize:]

	// check packet's dest ip
	if (hdr.Dst == iface.IP) {
		// packet has reached destination

		// create packet to pass into callback
		packet := &IPPacket{
			Header : *hdr,
			Payload : message,
		}
		// calling callback
		stack.Handler_table[uint16(hdr.Protocol)](packet)
	} else {
		// packet has NOT reached dest yet

		// check TTL is still valid
		if (hdr.TTL <= 0) {
			// TTL invalid, drop packet
			return nil
		}

		value, exists := iface.Neighbors[hdr.Dst]
		if (exists) {
			// if packet destination is interface's neighbor, can send directly through UDP
			udpAddr := &net.UDPAddr{
				IP: net.IP(value.Addr().AsSlice()),
				Port: int(value.Port()),
			}

			// decrement TTL and recompute checksum
			hdr.TTL = hdr.TTL - 1
			hdr.Checksum = int(ComputeChecksum(hdrBytes, uint16(hdr.Checksum)))
			hdrBytes, err = hdr.Marshal()
			if (err != nil) {
				fmt.Println(err)
				return nil
			}

			buffer := make([]byte, 0, len(hdrBytes)+len(message))
			buffer = append(buffer, hdrBytes...)
			buffer = append(buffer, []byte(message)...)
			
			// write packet with new header to neighbor
			_, err := iface.Conn.WriteToUDP(buffer, udpAddr)
			if (err != nil) {
				fmt.Println(err)
				return err
			}
		} else {
			// dest is not one of interface's neighbors
			// call sendIP again
			return stack.SendIP(hdr.Src, hdr.TTL, uint16(hdr.Checksum), hdr.Dst, uint16(hdr.Protocol), message)
		}
	}
	return nil
}

func (stack *IPStack) TestPacketHandler(packet *IPPacket) {
	fmt.Println("Received test packet: Src: " + packet.Header.Src.String() +
		", Dst: " + packet.Header.Dst.String() +
		", TTL: " + strconv.Itoa(packet.Header.TTL) +
		", Data: " + string(packet.Payload))
}

func RIPPacketHandler() {
}

func (stack *IPStack) RegisterRecvHandler(protocolNum uint16, callbackFunc HandlerFunc) {
	stack.Handler_table[protocolNum] = callbackFunc
}
