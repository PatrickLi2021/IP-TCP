package protocol

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
	Udp       *net.UDPAddr                  // the UDP address of the interface on this host
	Down      bool                          // whether the interface is down or not
	Conn      *net.UDPConn                  // listen to incoming UDP packets
}

type ipCostInterfaceTuple struct {
	ip        netip.Addr
	cost      uint16
	Interface *Interface
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType   lnxconfig.RoutingMode
	Forward_table map[netip.Prefix]*ipCostInterfaceTuple // maps IP prefixes to ip-cost-interface tuple
	Handler_table map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces    map[string]*Interface                  // maps interface names to interfaces
	Mutex         sync.Mutex                             // for concurrency
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

		// Map interface name to interface struct for ip stack struct- TODO maybe change this map key
		stack.Interfaces[newInterface.Name] = &newInterface

		// new neighbors map for new interface
		newInterface.Neighbors = make(map[netip.Addr]netip.AddrPort)
	}

	// for all the node's neighbors, loop through and add to correct interfaces map
	for _, neighbor := range configInfo.Neighbors {
		// This is saying "I can reach this neighbor through this interface lnxInterface"
		interface_struct := stack.Interfaces[neighbor.InterfaceName]
		interface_struct.Neighbors[neighbor.DestAddr] = neighbor.UDPAddr
	}

	for _, iface := range stack.Interfaces {
		// Populate forwarding table with interfaces ON THIS NODE
		stack.Forward_table[iface.Prefix] = &ipCostInterfaceTuple{
			ip:        iface.IP,
			cost:      16,
			Interface: iface,
		}
	}

	// Add default route entry
	for prefix, address := range configInfo.StaticRoutes {
		stack.Forward_table[prefix] = &ipCostInterfaceTuple{
			ip:        address,
			cost:      16,
			Interface: nil,
		}
	}

	return nil
}

func (stack *IPStack) SendIP(src *Interface, TTL int, dest netip.Addr, protocolNum uint16, data []byte) error {
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
		Src:      src.IP,
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

	// Find longest prefix match
	destAddrPort := stack.findPrefixMatch(header.Dst)

	if destAddrPort == nil {
		// no match found, drop packet
		return nil
	}

	fmt.Println("in send, find prefix done")
	bytesWritten, err := src.Conn.WriteToUDP(bytesToSend, destAddrPort)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Printf("Sent %d bytes\n", bytesWritten)
	return nil
}

func ComputeChecksum(headerBytes []byte) uint16 {
	checksum := header.Checksum(headerBytes, 0)
	checksumInv := checksum ^ 0xffff
	return checksumInv
}

func (stack *IPStack) findPrefixMatch(addr netip.Addr) *net.UDPAddr {
	var longestMatch netip.Prefix // Changed to netip.Prefix instead of *netip.Prefix
	var bestTuple *ipCostInterfaceTuple = nil

	fmt.Println("\nin find prefix match, forward table = ")
	for pref, tuple := range stack.Forward_table {
		fmt.Println(pref)
		fmt.Println(tuple.ip)
		fmt.Println()
	}
	for pref, tuple := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				bestTuple = tuple
			}
		}
	}

	if bestTuple == nil {
		fmt.Println("no match 1")
		// no match found, drop packet
		return nil
	}

	// check tuple to see if we hit deafult case and need to re-look up ip in forwarding table
	if bestTuple.Interface != nil {
		// it is local, so look up the dest ip in best tuple's neighbors table:
		for ip, port := range bestTuple.Interface.Neighbors {
			if (ip == addr) {
				// Convert netip.AddrPort to *net.UDPAddr
				udpAddr := &net.UDPAddr{
					IP:   net.IP(port.Addr().AsSlice()),
					Port: int(port.Port()),
				}
				fmt.Println("\nin find prefix, next dest ip = ")
				fmt.Println(ip)
				return udpAddr
			}
		}
		// no match found, drop packet
		fmt.Println("no match 2")
		return nil
	} else {
		// hit default case, make exactly one additional lookup in the table (we are guaranteed to make at most 2 calls
		// here)
		fmt.Println("\nrecursive call here, ip = ")
		fmt.Println(bestTuple.ip)
		return stack.findPrefixMatch(bestTuple.ip)
	}
}

func (stack *IPStack) Receive(iface *Interface) error {
	buffer := make([]byte, MAX_PACKET_SIZE)
	_, _, err := iface.Conn.ReadFromUDP(buffer)
	if err != nil {
		fmt.Println(err)
		return err
	}

	hdr, err := ipv4header.ParseHeader(buffer)
	if err != nil {
		fmt.Println("Error parsing header", err)
		return nil // TODO? what to do? node should not crash or exit, instead just drop message and return to processing
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
	if hdr.Dst == iface.IP {
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
		return stack.SendIP(iface, hdr.TTL-1, hdr.Dst, uint16(hdr.Protocol), message)
	}
	return nil
}

func TestPacketHandler(packet *IPPacket) {
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

// REPL commands
func (stack *IPStack) Li() string {
	var res = "Name Addr/Prefix State"
	for ifaceName, iface := range stack.Interfaces {
		res += "\n" + ifaceName + " " + iface.Prefix.String()
		if iface.Down {
			res += " down"
		} else {
			res += " up"
		}
	}
	return res
}

func (stack *IPStack) Ln() string {
	var res = "Iface VIP UDPAddr"
	for ifaceName, iface := range stack.Interfaces {
		if iface.Down {
			continue
		} else {
			for neighborIp, neighborAddrPort := range iface.Neighbors {
				res += "\n" + ifaceName + " " + neighborIp.String() + " " + neighborAddrPort.String()
			}
		}
	}
	return res
}

func (stack *IPStack) Lr() string {
	return ""
	// TODO
}

func (stack *IPStack) Down() error {
	return nil
	// TODO
}
