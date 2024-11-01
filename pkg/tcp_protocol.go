package protocol

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"strconv"
	"tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

type TCPState int

const (
	MaxMessageSize = 1400
)

type TCPListener struct {
	ID         uint16
	State      string
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
	Channel    chan *TCPConn
}

type TCPConn struct {
	ID         uint16
	State      string
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
	SeqNum     uint32
}

type TCPStack struct {
	ListenTable      map[uint16]*TCPListener
	ConnectionsTable map[FourTuple]*TCPConn
	IP               netip.Addr
	NextSocketID     uint16 // unique ID for each sockets per node
	IPStack          IPStack
	Channel          chan *TCPConn
}

type FourTuple struct {
	remotePort uint16
	remoteAddr netip.Addr
	srcPort    uint16
	srcAddr    netip.Addr
}

func (tcpStack *TCPStack) TCPHandler(packet *IPPacket) {
	// Retrieve the IP header and IP payload (which contains TCP header and TCP payload)
	ipHdr := packet.Header
	tcpHeaderAndData := packet.Payload

	// Parse TCP header into a struct and get the TCP payload
	tcpHdr := iptcp_utils.ParseTCPHeader(tcpHeaderAndData)
	tcpPayload := tcpHeaderAndData[tcpHdr.DataOffset:]

	// Retrieve and verify TCP checksum
	tcpChecksumFromHeader := tcpHdr.Checksum
	tcpHdr.Checksum = 0
	tcpComputedChecksum := iptcp_utils.ComputeTCPChecksum(&tcpHdr, ipHdr.Src, ipHdr.Dst, tcpPayload)

	if tcpComputedChecksum != tcpChecksumFromHeader {
		fmt.Println("checksum is not correct")
		return
	}

	// Get the port and flags
	fourTuple := FourTuple{
		remotePort: tcpHdr.SrcPort,
		remoteAddr: ipHdr.Src,
		srcPort:    tcpHdr.DstPort,
		srcAddr:    ipHdr.Dst,
	}

	tcpConn, normal_exists := tcpStack.ConnectionsTable[fourTuple]

	listenConn, listen_exists := tcpStack.ListenTable[fourTuple.srcPort]
	if normal_exists {
		if tcpHdr.Flags == (header.TCPFlagSyn | header.TCPFlagAck) {
			// Send ACK back to server
			flags := header.TCPFlagAck
			// TODO per notes should the SEQ bec just ack or ack + 1?
			err := tcpConn.sendTCP([]byte{}, uint32(flags), tcpHdr.AckNum, tcpHdr.SeqNum+1)
			if err != nil {
				fmt.Println("Could not sent SYN packet")
				return
			}
			tcpConn.State = "ESTABLISHED"
		}
		if tcpHdr.Flags == header.TCPFlagAck {
			// update socket state to established
			tcpConn.State = "ESTABLISHED"

			// TODO: communicate that handshake is done thru channeL?
			listenConn.Channel <- tcpConn
		}
		return
	} else if listen_exists {
		// create new connection
		if tcpHdr.Flags != header.TCPFlagSyn {
			// drop packet because only syn flag should be set and other flags are set
			return
		}

		// valid syn flag
		// Create new normal socket
		seqNum := int(rand.Uint32())
		tcpConn := &TCPConn{
			State:      "SYN_RECEIVED",
			LocalPort:  tcpHdr.DstPort,
			LocalAddr:  tcpStack.IP,
			RemotePort: tcpHdr.SrcPort,
			RemoteAddr: ipHdr.Src,
			TCPStack:   tcpStack,
			SeqNum:     uint32(seqNum),
		}

		// add the new normal socket to tcp stack's connections table
		tuple := FourTuple{
			remotePort: tcpConn.RemotePort,
			remoteAddr: tcpConn.RemoteAddr,
			srcPort:    tcpConn.LocalPort,
			srcAddr:    tcpConn.LocalAddr,
		}
		tcpStack.ConnectionsTable[tuple] = tcpConn

		// increment next socket id - used when listing out all sockets
		tcpStack.NextSocketID++

		// Send a SYN-ACK back to client
		flags := header.TCPFlagSyn | header.TCPFlagAck
		err := tcpConn.sendTCP([]byte{}, uint32(flags), uint32(seqNum), uint32(tcpHdr.SeqNum+1))
		if err != nil {
			fmt.Println("Error - Could not send SYN-ACK back")
			return
		}

	} else {
		// drop packet
		return
	}
}

func (tcpStack *TCPStack) Initialize(localIP netip.Addr, ipStack *IPStack) {
	tcpStack.IPStack = *ipStack
	tcpStack.IP = localIP
	tcpStack.ListenTable = make(map[uint16]*TCPListener)
	tcpStack.ConnectionsTable = make(map[FourTuple]*TCPConn)
	tcpStack.NextSocketID = 0

	// register tcp packet handler
	tcpStack.IPStack.RegisterRecvHandler(6, tcpStack.TCPHandler)
}

func formatAddr(addr netip.Addr) string {
	// Check if addr is equal to the zero value of netip.Addr
	if !addr.IsValid() {
		return "*"
	}
	return addr.String()
}

func (tcpStack *TCPStack) ListSockets() {
	uniqueId := 0
	fmt.Println("SID  LAddr           LPort      RAddr          RPort    Status")
	// Loop through all listener sockets on this node
	for _, socket := range tcpStack.ListenTable {
		localAddrStr := formatAddr(socket.LocalAddr)
		remoteAddrStr := formatAddr(socket.RemoteAddr)
		fmt.Println(strconv.Itoa(uniqueId) + "    " + localAddrStr + "         " + strconv.Itoa(int(socket.LocalPort)) + "       " + remoteAddrStr + "        " + strconv.Itoa(int(socket.RemotePort)) + "        " + socket.State)
		uniqueId++
	}
	// Loop through all connection sockets on this node
	for _, socket := range tcpStack.ConnectionsTable {
		fmt.Println(strconv.Itoa(uniqueId) + "    " + socket.LocalAddr.String() + "        " + strconv.Itoa(int(socket.LocalPort)) + "      " + socket.RemoteAddr.String() + "       " + strconv.Itoa(int(socket.RemotePort)) + "     " + socket.State)
		uniqueId++
	}
}

func (tcpStack *TCPStack) ACommand(port uint16) {
	listenConn, err := tcpStack.VListen(port)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Created listen socket")
	for {
		_, err := listenConn.VAccept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("listen conn created")
		// else {
		// 	go conn.VRead()???
		// }
	}
}

func (tcpConn *TCPConn) VRead(buf []byte) (int, error) {
	return 0, nil
}

func (tcpStack *TCPStack) CCommand(ip netip.Addr, port uint16) {
	_, err := tcpStack.VConnect(ip, port)
	if err != nil {
		fmt.Println(err)
		return
	}
}
