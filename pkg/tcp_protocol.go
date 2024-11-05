package protocol

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

type FourTuple struct {
	remotePort uint16
	remoteAddr netip.Addr
	srcPort    uint16
	srcAddr    netip.Addr
}

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
	SendBuf	   *TCPSendBuffer
	RecvBuf	   *TCPRecvBuffer
	SpaceOpen chan 
	// buffers, initial seq num
	// sliding window (send): some list or queue of in flight packets for retransmit
	// rec side: out of order packets to track missing packets
}

type TCPStack struct {
	ListenTable      map[uint16]*TCPListener
	ConnectionsTable map[FourTuple]*TCPConn
	IP               netip.Addr
	NextSocketID     uint16 // unique ID for each sockets per node
	IPStack          IPStack
	Channel          chan *TCPConn
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
		if tcpHdr.Flags == (header.TCPFlagSyn | header.TCPFlagAck) && tcpConn.State == "SYN_SENT" {
			// Send ACK back to server
			flags := header.TCPFlagAck
			err := tcpConn.sendTCP([]byte{}, uint32(flags), tcpHdr.AckNum, tcpHdr.SeqNum+1)
			if err != nil {
				fmt.Println("Could not sent ACK back")
				return
			}
			tcpConn.State = "ESTABLISHED"
		}
		if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "SYN_RECEIVED" {
			// update socket state to established
			tcpConn.State = "ESTABLISHED"
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
		SendBuf := &TCPBuffer{
			Buffer: make([]byte, BUFFER_SIZE),
			UNA: 0,
			NXT: 0,
			LBW: 0,
			Channel: make(chan *TCPConn), // TODO subject to change
		}
		tcpConn := &TCPConn{
			State:      "SYN_RECEIVED",
			LocalPort:  tcpHdr.DstPort,
			LocalAddr:  tcpStack.IP,
			RemotePort: tcpHdr.SrcPort,
			RemoteAddr: ipHdr.Src,
			TCPStack:   tcpStack,
			SeqNum:     uint32(seqNum),
			SendBuf: SendBuf,
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