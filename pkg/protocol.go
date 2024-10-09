package main

import (
	"errors"
	"fmt"
	"ip-ip-pa/lnxconfig"
	"net"
	"net/netip"
	"strconv"
	"sync"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

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

type costInterfacePair struct {
	Interface *Interface
	cost      uint16
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType   lnxconfig.RoutingMode
	Forward_table map[netip.Prefix]*costInterfacePair // maps IP prefixes to cost-interface pair
	Handler_table map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces    map[string]*Interface               // maps interface names to interfaces
	Mutex         sync.Mutex                          // for concurrency
}

func (stack *IPStack) Initialize(configInfo lnxconfig.IPConfig) error {
	// Populate fields of stack
	stack.RoutingType = configInfo.RoutingMode
	stack.Forward_table = make(map[netip.Prefix]*costInterfacePair)
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
		conn, err := net.DialUDP("udp4", nil, serverAddr)
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
	dest_interface := stack.findPrefixMatch(header.Dst)

	bytesWritten, err := dest_interface.Conn.WriteToUDP(bytesToSend, dest_interface.Udp)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
	return nil
}

func (stack *IPStack) findPrefixMatch(addr netip.Addr) *Interface {
	var longestMatch netip.Prefix // Changed to netip.Prefix instead of *netip.Prefix
	var dest_interface *Interface
	for pref, costInterfacePair := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				dest_interface = costInterfacePair.Interface
			}
		}
	}
	return dest_interface
}

func (stack *IPStack) Receive(packet *IPPacket) error {
	// Decrement TTL by 1 and recompute checksum
	packet.Header.TTL -= 1
	headerBytes, err := packet.Header.Marshal()
	if err != nil {
		fmt.Println(err)
		return err
	}
	recomputedChecksum := ComputeChecksum(headerBytes)
	if recomputedChecksum != uint16(packet.Header.Checksum) {
		return errors.New("Invalid packet, malformed checksum")
	}
	// If the packet is local, we look up the IP in the interface's neighbor table
	// and look it up there (send it to that neighbor's UDP port. This is in the config file)
	// Check if packet was destined for this node by going through all interfaces and neighbors on this node
	for _, iface := range stack.Interfaces {
		if iface.IP == packet.Header.Dst {
			if packet.Header.Protocol == 0 {
				if callbackFunction, exists := stack.Handler_table[0]; exists {
					callbackFunction(packet)
					return nil
				}
			}
		} else {
			for neighbor_ip, _ := range iface.Neighbors {
				if neighbor_ip == packet.Header.Dst {
					if packet.Header.Protocol == 0 {
						if callbackFunction, exists := stack.Handler_table[0]; exists {
							callbackFunction(packet)
							return nil
						}
					}
				}
			}
		}
	}
	// Packet was unable to be delivered locally, so it needs to be forwarded to the next hop destination
	stack.SendIP(packet.Header.Dst, uint16(packet.Header.Protocol), packet.Payload)
	return nil
}

func ComputeChecksum(headerBytes []byte, prevChecksum uint16) uint16 {
	checksum := header.Checksum(headerBytes, prevChecksum)
	checksumInv := checksum ^ 0xffff
	return checksumInv
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
