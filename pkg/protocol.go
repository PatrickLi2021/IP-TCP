package main

import (
	"fmt"
	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
	"ip-ip-pa/lnxconfig"
	"net"
	"net/netip"
	"strconv"
	"sync"
)

type HandlerFunc = func(*IPPacket, []interface{})

type IPPacket struct {
	Header  ipv4header.IPv4Header
	Payload []byte
}

type Interface struct {
	Name      string                // the name of the interface
	IP        netip.Addr            // the IP address of the interface on this host
	Prefix    netip.Prefix          // the network submask/prefix
	Neighbors map[netip.Addr]string // maps (virtual) IPs to interface name
	Udp       netip.AddrPort        // the UDP address of the interface on this host
	Down      bool                  // whether the interface is down or not
	Conn      *net.UDPConn          // listen to incoming UDP packets
}

type costInterfacePair struct {
	Interface *Interface
	cost      uint16
}

type IPStack struct {
	RoutingType   lnxconfig.RoutingMode
	Forward_table map[netip.Prefix]*costInterfacePair // maps IP prefixes to cost-interface pair
	Handler_table map[int]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces    map[string]*Interface               // maps interface names to interfaces
	Mutex         sync.Mutex                          // for concurrency
}

func (stack *IPStack) Initialize(configInfo lnxconfig.IPConfig) error {
	// Populate fields of stack
	stack.RoutingType = configInfo.RoutingMode

	// Go through each interface to populate map of interfaces for IPStack struct
	for _, lnxInterface := range configInfo.Interfaces {
		newInterface := Interface{
			Name:   lnxInterface.Name,
			IP:     lnxInterface.AssignedIP,
			Prefix: lnxInterface.AssignedPrefix,
			Udp:    lnxInterface.UDPAddr,
			Down:   false,
		}

		// creating udp conn for each interface
		serverAddr, err := net.ResolveUDPAddr("udp4", lnxInterface.UDPAddr.String())
		if err != nil {
			fmt.Println(err)
			return err
		}
		conn, err := net.DialUDP("udp4", nil, serverAddr)
		if err != nil {
			fmt.Println(err)
			return err
		}
		newInterface.Conn = conn

		// Map interface name to interface struct - TODO maybe change this map key
		stack.Interfaces[newInterface.Name] = &newInterface
	}

	// Go through each neighbor and populate a list of neighbors for IPStack struct
	interfaceNeighbors := []*Interface{}
	for _, neighbor := range configInfo.Neighbors {
		_, exists := stack.Interfaces[neighbor.InterfaceName]
		if exists {
			interfaceNeighbors = append(interfaceNeighbors, stack.Interfaces[neighbor.InterfaceName])
		}
	}
	return nil
}

func (stack *IPStack) SendIP(dest netip.Addr, protocolNum uint16, data []byte) error {
	// Construct IP packet header
	header := ipv4header.IPv4Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4header.HeaderLen + len(data),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      16,
		Protocol: int(protocolNum),
		Checksum: 0, // Should be 0 until checksum is computed
		Src:      netip.MustParseAddr("10.0.0.1"),
		Dst:      dest,
		Options:  []byte{},
	}
	headerBytes, err := header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}
	// Compute header checksum
	header.Checksum = int(ComputeChecksum(headerBytes)) + 1
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
	dest_interface := findPrefixMatch(stack.Forward_table, header.Dst)
	remote_addr, err := net.ResolveUDPAddr("udp4", dest_interface.Udp.String())
	if err != nil {
		fmt.Println(err)
	}

	bytesWritten, err := dest_interface.Conn.WriteToUDP(bytesToSend, remote_addr)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)

}

func (stack *IPStack) Receive(packet *IPPacket) {
	// Check if packet was destined for this node by going through all interfaces on this node
	for _, iface := range stack.Interfaces {
		if iface.IP == dest {
			if packet.Header.Protocol == 0 {
				if callbackFunction, exists := stack.Handler_table[0]; exists {
					callbackFunction()
				}
			}
		}
	}
}

func ComputeChecksum(headerBytes []byte) uint16 {
	checksum := header.Checksum(headerBytes, 0)
	checksumInv := checksum ^ 0xffff
	return checksumInv
}

func findPrefixMatch(forward_table map[netip.Prefix]*costInterfacePair, addr netip.Addr) *Interface {
	var longestMatch netip.Prefix // Changed to netip.Prefix instead of *netip.Prefix
	var dest_interface *Interface
	for pref, costInterfacePair := range forward_table {
		if pref.Contains(addr) {
			if pref.IsValid() || pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				dest_interface = costInterfacePair.Interface
			}
		}
	}
	return dest_interface
}

func (stack *IPStack) TestPacketHandler(packet *IPPacket) {
	fmt.Println("Received test packet: Src: " + packet.Header.Src.String() +
		", Dst: " + packet.Header.Dst.String() +
		", TTL: " + strconv.Itoa(packet.Header.TTL) +
		", Data: " + string(packet.Payload))
}

func RIPPacketHandler() {

}
