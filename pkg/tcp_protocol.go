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
	ID          uint16
	State       string
	LocalPort   uint16
	LocalAddr   netip.Addr
	RemotePort  uint16
	RemoteAddr  netip.Addr
	TCPStack    *TCPStack
	ConnCreated chan *TCPConn
}

type TCPConn struct {
	ID                uint16
	State             string
	LocalPort         uint16
	LocalAddr         netip.Addr
	RemotePort        uint16
	RemoteAddr        netip.Addr
	TCPStack          *TCPStack
	SeqNum            uint32
	SendBuf           *TCPSendBuffer
	RecvBuf           *TCPRecvBuffer
	SendSpaceOpen     chan bool
	RecvSpaceOpen     chan bool
	SendBufferHasData chan bool
	ISN               uint32
	ACK               uint32
	AckReceived       chan uint32
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
	SocketIDToConn   map[uint32]*FourTuple
	Channel          chan *TCPConn
}

func (tcpStack *TCPStack) Initialize(localIP netip.Addr, ipStack *IPStack) {
	tcpStack.IPStack = *ipStack
	tcpStack.IP = localIP
	tcpStack.ListenTable = make(map[uint16]*TCPListener)
	tcpStack.ConnectionsTable = make(map[FourTuple]*TCPConn)
	tcpStack.SocketIDToConn = make(map[uint32]*FourTuple)
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
		if tcpHdr.Flags == (header.TCPFlagSyn|header.TCPFlagAck) && tcpConn.State == "SYN_SENT" {
			// Send ACK back to server
			flags := header.TCPFlagAck
			err := tcpConn.sendTCP([]byte{}, uint32(flags), tcpHdr.AckNum, tcpConn.ACK)

			if err != nil {
				fmt.Println("Could not sent ACK back")
				return
			}
			tcpConn.State = "ESTABLISHED"
			// Start new goroutine monitoring TCPConn's send buffer for new data to be sent
			go tcpConn.SendSegment()
			go tcpConn.WatchRecvBuf()
		}
		if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "SYN_RECEIVED" {
			// 'active' server receives ACK
			// update socket state to established
			tcpConn.State = "ESTABLISHED"
			listenConn.ConnCreated <- tcpConn
			// Start new goroutine monitoring TCPConn's send buffer for new data to be sent
			go tcpConn.SendSegment()
			// Start new goroutine continuously checking TCPConn's receive buffer for space
			go tcpConn.WatchRecvBuf()
		}
		if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "ESTABLISHED" {
			// If we've received an ACK packet and we're in the ESTABLISHED state, send packet
			// through TCPConn saying ACK has been received (this is for sending/receiving actual data)
			tcpConn.AckReceived <- tcpHdr.AckNum

			// Calculate remaining space in buffer
			remainingSpace := BUFFER_SIZE - (int(tcpHdr.SeqNum) - int(tcpConn.RecvBuf.LBR))
			if len(tcpPayload) > remainingSpace {
				fmt.Println("Data received is larger than remaining space in receive buffer")
			} else {
				// Copy data into receive buffer
				bufferPointer := int(tcpHdr.SeqNum) - int(tcpConn.RecvBuf.LBR)
				copy(tcpConn.RecvBuf.Buffer[bufferPointer:bufferPointer+len(tcpPayload)], tcpPayload)
				tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))
				// Send signal that bytes are now in receive buffer
				tcpConn.RecvSpaceOpen <- true
			}
		}
		return
	} else if listen_exists {
		// create new connection
		if tcpHdr.Flags != header.TCPFlagSyn {
			// drop packet because only syn flag should be set and other flags are set
			return
		}

		// Server receives syn in handshake from client
		// Create new normal socket
		seqNum := int(rand.Uint32())
		SendBuf := &TCPSendBuffer{
			Buffer:  make([]byte, BUFFER_SIZE),
			UNA:     0,
			NXT:     0,
			LBW:     0,
			Channel: make(chan bool), // TODO subject to change
		}
		RecvBuf := &TCPRecvBuffer{
			Buffer: make([]byte, BUFFER_SIZE),
			NXT:    0,
			LBR:    0,
		}
		tcpConn := &TCPConn{
			ID:                tcpStack.NextSocketID,
			State:             "SYN_RECEIVED",
			LocalPort:         tcpHdr.DstPort,
			LocalAddr:         tcpStack.IP,
			RemotePort:        tcpHdr.SrcPort,
			RemoteAddr:        ipHdr.Src,
			TCPStack:          tcpStack,
			SeqNum:            uint32(seqNum),
			ISN:               uint32(seqNum),
			SendBuf:           SendBuf,
			RecvBuf:           RecvBuf,
			SendSpaceOpen:     make(chan bool),
			RecvSpaceOpen:     make(chan bool),
			SendBufferHasData: make(chan bool),
		}

		// add the new normal socket to tcp stack's connections table
		tuple := FourTuple{
			remotePort: tcpConn.RemotePort,
			remoteAddr: tcpConn.RemoteAddr,
			srcPort:    tcpConn.LocalPort,
			srcAddr:    tcpConn.LocalAddr,
		}
		tcpStack.ConnectionsTable[tuple] = tcpConn
		tcpStack.SocketIDToConn[uint32(tcpStack.NextSocketID)] = &tuple
		// increment next socket id - used when listing out all sockets
		tcpStack.NextSocketID++

		// Send a SYN-ACK back to client
		flags := header.TCPFlagSyn | header.TCPFlagAck
		err := tcpConn.sendTCP([]byte{}, uint32(flags), uint32(seqNum), tcpConn.ACK)
		if err != nil {
			fmt.Println("Error - Could not send SYN-ACK back")
			return
		}

	} else {
		// drop packet
		return
	}
}
