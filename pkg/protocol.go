package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
	"ip-ip-pa/lnxconfig"
	"net"
	"net/netip"
	"strconv"
	"sync"
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
	cost      uint32
	Interface *Interface
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType     lnxconfig.RoutingMode
	Forward_table   map[netip.Prefix]*ipCostInterfaceTuple // maps IP prefixes to ip-cost-interface tuple
	Handler_table   map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces      map[netip.Addr]*Interface              // maps interface names to interfaces
	Mutex           sync.Mutex                             // for concurrency
	NameToInterface map[string]*Interface                  // maps name to interface for REPL commands
}

func (stack *IPStack) Initialize(configInfo lnxconfig.IPConfig) error {
	// Populate fields of stack
	stack.RoutingType = configInfo.RoutingMode
	stack.Forward_table = make(map[netip.Prefix]*ipCostInterfaceTuple)
	stack.Handler_table = make(map[uint16]HandlerFunc)
	stack.Interfaces = make(map[netip.Addr]*Interface)
	stack.NameToInterface = make(map[string]*Interface)
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
	for _, iface := range stack.Interfaces {
		// Populate forwarding table with interfaces ON THIS NODE
		stack.Forward_table[iface.Prefix] = &ipCostInterfaceTuple{
			ip:        iface.IP,
			cost:      16,
			Interface: iface,
		}
	}

	// Add default route entry only if routing type is NOT RIP (aka NOT 2)
	if stack.RoutingType != 2 {
		for prefix, address := range configInfo.StaticRoutes {
			stack.Forward_table[prefix] = &ipCostInterfaceTuple{
				ip:        address,
				cost:      16,
				Interface: nil,
			}
		}
	}

	return nil
}

func (stack *IPStack) SendIP(src *netip.Addr, TTL int, dest netip.Addr, protocolNum uint16, data []byte) error {
	fmt.Println("In SendIP()")
	fmt.Println(src)
	fmt.Println(dest)
	fmt.Printf("This is the destination: %s\n", dest.String())

	// Find longest prefix match
	srcIP, destAddrPort := stack.findPrefixMatch(src, dest)
	fmt.Println("Finished findPrefixMatch")
	fmt.Println(destAddrPort)
	fmt.Printf("This is where I'm forwarding the packet: %d\n", destAddrPort.Port)
	if destAddrPort == nil {
		// no match found, drop packet
		return nil
	}
	fmt.Println()

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
		Src:      *srcIP,
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

	// TODO
	bytesWritten, err := (stack.Interfaces[*srcIP].Conn).WriteToUDP(bytesToSend, destAddrPort)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Printf("This is the destination port: %d\n", destAddrPort.Port)
	fmt.Printf("Sent %d bytes\n", bytesWritten)
	return nil
}

func ComputeChecksum(headerBytes []byte) uint16 {
	checksum := header.Checksum(headerBytes, 0)
	checksumInv := checksum ^ 0xffff
	return checksumInv
}

func (stack *IPStack) findPrefixMatch(src *netip.Addr, addr netip.Addr) (*netip.Addr, *net.UDPAddr) {
	var longestMatch netip.Prefix // Changed to netip.Prefix instead of *netip.Prefix
	var bestTuple *ipCostInterfaceTuple = nil

	for pref, tuple := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				bestTuple = tuple
			}
		}
	}

	if bestTuple == nil {
		// no match found, drop packet
		return nil, nil
	}

	// check tuple to see if we hit default case and need to re-look up ip in forwarding table
	if bestTuple.Interface != nil {
		// it is local, so look up the dest ip in best tuple's neighbors table:
		for ip, port := range bestTuple.Interface.Neighbors {
			if ip == addr {
				// Convert netip.AddrPort to *net.UDPAddr
				udpAddr := &net.UDPAddr{
					IP:   net.IP(port.Addr().AsSlice()),
					Port: int(port.Port()),
				}
				if src == nil {
					// if src needs to be determined, it is the interface that we use to look thru its neighbors
					return &bestTuple.Interface.IP, udpAddr
				} else {
					// already has a src
					return src, udpAddr
				}
			}
		}
		// no match found, drop packet
		return nil, nil
	} else {
		// hit default case, make exactly one additional lookup in the table (we are guaranteed to make at most 2 calls
		// here)
		return stack.findPrefixMatch(src, bestTuple.ip)
	}
}

func (stack *IPStack) Receive(iface *Interface) error {
	fmt.Println("In Receive()")
	buffer := make([]byte, MAX_PACKET_SIZE)
	_, _, err := iface.Conn.ReadFromUDP(buffer)
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println("Just read from the UDP conn")
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
	fmt.Println("finished validating checksum")
	// if TTL < 0, drop packet
	if hdr.TTL <= 0 {
		return nil
	}

	// packet payload
	message := buffer[hdrSize:]
	fmt.Println("about to check the packet's destination IP")
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
		fmt.Println("calling send IP")
		fmt.Printf("This is the source: %s\n", hdr.Src.String())
		fmt.Printf("This is the destination: %s\n", hdr.Dst.String())
		return stack.SendIP(&hdr.Src, hdr.TTL-1, hdr.Dst, uint16(hdr.Protocol), message)
	}
	return nil
}

func TestPacketHandler(packet *IPPacket) {
	fmt.Println("Received test packet: Src: " + packet.Header.Src.String() +
		", Dst: " + packet.Header.Dst.String() +
		", TTL: " + strconv.Itoa(packet.Header.TTL) +
		", Data: " + string(packet.Payload))
}

func (stack *IPStack) RIPPacketHandler(packet *IPPacket) {
	ripPacket, err := UnmarshalRIP(packet.Payload)
	if err != nil {
		fmt.Println("error unmarshaling packet payload in rip packet handler")
		fmt.Println(err)
		return
	}

	if ripPacket.Command == 1 {
		// Have received a RIP request, need to send out an update
		destIP := packet.Header.Dst

		ripUpdate := &RIPPacket{
			Command:     2,
			Num_entries: uint16(len(stack.Forward_table)),
		}

		entries := make([]RIPEntry, ripUpdate.Num_entries)
		for mask, tuple := range stack.Forward_table {
			// Convert IP address into uint32
			ipInteger, _, _ := convertToUint32(tuple.ip)

			// Convert prefix/mask into uint32
			prefixInteger, prefixLen, _ := convertToUint32(mask)
			entry := RIPEntry{
				Cost:    uint32(tuple.cost),
				Address: ipInteger,
				Mask:    prefixInteger,
				MaskLen: prefixLen,
			}
			entries = append(entries, entry)
		}

		ripUpdate.Entries = entries

		ripBytes, err := MarshalRIP(ripUpdate)
		if err != nil {
			fmt.Println("error marshaling rip packet in rip packet handler")
			fmt.Println(err)
			return
		}
		stack.SendIP(nil, 16, destIP, 200, ripBytes)
	} else if ripPacket.Command == 2 {
		// received response, will need to update routing table
		entryUpdates := ripPacket.Entries
		for i := 0; i < int(ripPacket.Num_entries); i++ {
			entry := entryUpdates[i]
			entryMask, _ := uint32ToPrefix(entry.Mask, int(entry.MaskLen))
			entryAddress, _ := uint32ToAddr(entry.Address)

			stack.Forward_table[entryMask] = &ipCostInterfaceTuple{
				ip:        entryAddress,
				cost:      entry.Cost,
				Interface: stack.Interfaces[entryAddress],
			}
		}
	}
}

func (stack *IPStack) RegisterRecvHandler(protocolNum uint16, callbackFunc HandlerFunc) {
	stack.Handler_table[protocolNum] = callbackFunc
}

// REPL commands
func (stack *IPStack) Li() string {
	var res = "Name Addr/Prefix State"
	for _, iface := range stack.Interfaces {
		res += "\n" + iface.Name + " " + iface.Prefix.String()
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
	for _, iface := range stack.Interfaces {
		if iface.Down {
			continue
		} else {
			for neighborIp, neighborAddrPort := range iface.Neighbors {
				res += "\n" + iface.Name + " " + neighborIp.String() + " " + neighborAddrPort.String()
			}
		}
	}
	return res
}

func (stack *IPStack) Lr() string {
	return ""
}

func (stack *IPStack) Down(interfaceName string) {
	// Close the interface conn
	iface := stack.NameToInterface[interfaceName]
	iface.Conn.Close()

	// Set down flag in interface to true
	iface.Down = true
}

func (stack *IPStack) Up(interfaceName string) {
	// Set interface status to active as long as interface was down in the first place
	iface := stack.NameToInterface[interfaceName]
	if iface.Down {
		iface.Down = false
		// Start listening again
		iface.Conn, _ = net.ListenUDP("udp4", iface.Udp)
	}
}

func convertToUint32(input interface{}) (uint32, uint32, error) {
	switch v := input.(type) {
	case netip.Prefix:
		if v.Addr().Is4() {
			ip := v.Addr().As4()
			return binary.BigEndian.Uint32(ip[:]), uint32(v.Bits()), nil
		}
		return 0, 0, fmt.Errorf("prefix is not IPv4")
	case netip.Addr:
		if v.Is4() {
			ip := v.As4()
			return binary.BigEndian.Uint32(ip[:]), 0, nil
		}
		return 0, 0, fmt.Errorf("address is not IPv4")
	default:
		return 0, 0, fmt.Errorf("unsupported type: %T", v)
	}
}

func uint32ToAddr(ipUint32 uint32) (netip.Addr, error) {
	// Convert uint32 to byte slice
	ip := make([]byte, 4)
	binary.BigEndian.PutUint32(ip, ipUint32)

	// Create a netip.Addr from the byte slice
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, errors.New("could not create netip.addr")
	}
	return addr, nil
}

func uint32ToPrefix(ipUint32 uint32, prefixLen int) (netip.Prefix, error) {
	addr, err := uint32ToAddr(ipUint32)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, prefixLen), nil
}
