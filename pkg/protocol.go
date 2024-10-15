package protocol

import (
	"bytes"
	"encoding/binary"
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
	Mutex     sync.RWMutex
}

type ipCostInterfaceTuple struct {
	NextHopIP netip.Addr
	Cost      uint32
	Interface *Interface
	Type      string
}

type HandlerFunc = func(*IPPacket)

type IPStack struct {
	RoutingType     lnxconfig.RoutingMode
	Forward_table   map[netip.Prefix]*ipCostInterfaceTuple // maps IP prefixes to ip-cost-interface tuple
	Handler_table   map[uint16]HandlerFunc                 // maps protocol numbers to handlers
	Interfaces      map[netip.Addr]*Interface              // maps interface names to interfaces
	Mutex           sync.RWMutex                           // for concurrency
	NameToInterface map[string]*Interface                  // maps name to interface for REPL commands
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

	// Populate forwarding table with interfaces ON THIS NODE
	for _, iface := range stack.Interfaces {
		stack.Forward_table[iface.Prefix] = &ipCostInterfaceTuple{
			NextHopIP: iface.IP,
			Cost:      0,
			Interface: iface,
			Type: "L",
		}
	}

	// Add default route entry only if routing type is NOT RIP (aka NOT 2)
	if stack.RoutingType != 2 {
		for prefix, address := range configInfo.StaticRoutes {
			stack.Forward_table[prefix] = &ipCostInterfaceTuple{
				NextHopIP: address,
				Cost:      0, // TODO
				Interface: nil,
				Type: "R",
			}
		}
	}
	return nil
}

func (stack *IPStack) SendIP(originalSrc *netip.Addr, TTL int, dest netip.Addr, protocolNum uint16, data []byte) error {
	// check if sending to itself
	for ip := range stack.Interfaces {
		fmt.Println(ip)
		if (ip == dest) {
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
	fmt.Println("in send ip, src = ")
	fmt.Println(*srcIP)
	fmt.Println(destAddrPort)
	if destAddrPort == nil {
		// no match found, drop packet
		return nil
	}
	iface, exists := stack.Interfaces[*srcIP]
	if exists && iface.Down {
		// drop packet, if src is down
		return nil
	}

	if (originalSrc == nil) {
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
	for pref, tuple := range stack.Forward_table {
		if pref.Contains(addr) {
			if pref.Bits() > longestMatch.Bits() {
				longestMatch = pref
				bestTuple = tuple
			}
		}
	}

	// no match found, drop packet
	if bestTuple == nil {
		// should never reach this
		fmt.Println("A")
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
	fmt.Println("D")
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
	correctDest := hdr.Dst == iface.IP
	fmt.Println("in receive, current iface ip = ")
	fmt.Println(iface.IP)
	fmt.Println("dest ip = ")
	fmt.Println(hdr.Dst)
	fmt.Println("neighbors")
	for interIP, _ := range stack.Interfaces {
		fmt.Println(interIP)
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
		destIP := packet.Header.Src

		ripUpdate := &RIPPacket{
			Command: 2,
		}

		entries := make([]RIPEntry, 0)
		for prefix, tuple := range stack.Forward_table {

			if tuple.Interface == nil && tuple.Cost == 0 {
				// forward table entry is a default route only, we don't send to other routers
				continue
			}

			// Convert IP address into uint32)
			address, _, _ := ConvertToUint32(prefix.Addr())
			mask, _, _ := ConvertToUint32(prefix.Masked())

			entry := RIPEntry{
				Cost:    uint32(tuple.Cost),
				Address: address,
				Mask:    mask,
			}
			entries = append(entries, entry)
		}
		ripUpdate.Entries = entries
		ripUpdate.Num_entries = uint16(len(entries))

		ripBytes, err := MarshalRIP(ripUpdate)
		if err != nil {
			fmt.Println("error marshaling rip packet in rip packet handler")
			fmt.Println(err)
			return
		}
		stack.SendIP(nil, 31, destIP, 200, ripBytes)
	} else if ripPacket.Command == 2 {
		// received response, will need to update routing table
		entryUpdates := ripPacket.Entries
		for i := 0; i < int(ripPacket.Num_entries); i++ {
			entry := entryUpdates[i]
			entryAddress, err := uint32ToAddr(entry.Address)
			if err != nil {
				fmt.Println("error converting uint32 to net ip addr")
				fmt.Println(err)
				return
			}
			entryMask, err := uint32ToAddr(entry.Mask)
			if err != nil {
				fmt.Println("error converting uint32 to mask")
				fmt.Println(err)
				return
			}
			entryPrefix, err := entryAddress.Prefix(entryMask.BitLen() - 8)
			if err != nil {
				fmt.Println("error converting uint32 to net ip prefix")
				fmt.Println(err)
				return
			}
			prevTuple, exists := stack.Forward_table[entryPrefix]
			if !exists {
				// entry from neighbor does not exist, add to table
				stack.Forward_table[entryPrefix] = &ipCostInterfaceTuple{
					NextHopIP: packet.Header.Src,
					Cost:      entry.Cost + 1,
					Interface: nil,
				}
			} else if exists && entry.Cost+1 < prevTuple.Cost {
				// entry exists and updated cost is lower than old, update table with new entry
				stack.Forward_table[entryPrefix].Cost = entry.Cost
			} else if exists && entry.Cost+1 > prevTuple.Cost {
				// updated cost greater than old cost
				if packet.Header.Src == prevTuple.NextHopIP {
					// topology has changed, route has higher cost now, update table
					stack.Forward_table[entryPrefix].Cost = entry.Cost
					stack.Forward_table[entryPrefix].NextHopIP = packet.Header.Src
				}
			}
			// TODO: else new cost == old cost and dests are same, so refresh route
		}
	}
}

func (stack *IPStack) RegisterRecvHandler(protocolNum uint16, callbackFunc HandlerFunc) {
	stack.Handler_table[protocolNum] = callbackFunc
}

// REPL commands
func (stack *IPStack) Li() string {
	var res = "Name Addr/Prefix  State"
	for _, iface := range stack.Interfaces {
		res += "\n" + iface.Name + "  " + iface.Prefix.String()
		if iface.Down {
			res += "  down"
		} else {
			res += "  up"
		}
	}
	return res
}

func (stack *IPStack) Ln() string {
	var res = "Iface VIP        UDPAddr"
	for _, iface := range stack.Interfaces {
		if iface.Down {
			continue
		} else {
			for neighborIp, neighborAddrPort := range iface.Neighbors {
				res += "\n" + iface.Name + "   " + neighborIp.String() + "   " + neighborAddrPort.String()
			}
		}
	}
	return res
}

func (stack *IPStack) Lr() string {
	fmt.Println("in here")
	fmt.Println(stack.Forward_table)
	var res = "T     Prefix     Next hop    Cost"
	for prefix, ipCostIfaceTuple := range stack.Forward_table {
		cost_string := strconv.FormatUint(uint64(ipCostIfaceTuple.Cost), 10)
		res += "\n" + prefix.String() + "   " + ipCostIfaceTuple.NextHopIP.String() + "   " + cost_string
	}
	return res
}

func (stack *IPStack) Down(interfaceName string) {
	// Set down flag in interface to true
	iface, exists := stack.NameToInterface[interfaceName]
	if exists {
		iface.Down = true
	}
}

func (stack *IPStack) Up(interfaceName string) {
	iface, exists := stack.NameToInterface[interfaceName]
	if exists {
		iface.Down = false
	}
}

func ConvertToUint32(input interface{}) (uint32, uint32, error) {
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

	// Create a netip.Addr from the byte slice using the 4-byte IPv4 constructor
	addr := netip.AddrFrom4([4]byte{ip[3], ip[2], ip[1], ip[0]})
	return addr, nil
}

func uint32ToPrefix(ipUint32 uint32, prefixLen int) (netip.Prefix, error) {
	addr, err := uint32ToAddr(ipUint32)
	if err != nil {
		return netip.Prefix{}, err
	}
	return addr.Prefix(prefixLen)
}
