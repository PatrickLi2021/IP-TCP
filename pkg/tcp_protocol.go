package protocol

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
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
	SendBuf           *TCPSendBuf
	RecvBuf           *TCPRecvBuf
	SendSpaceOpen     chan bool
	RecvSpaceOpen     chan bool
	SendBufferHasData chan bool
	ISN               uint32
	ACK               uint32
	AckReceived       chan uint32
	CurWindow         uint16
	TotalBytesSent    uint32
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

		// Handshake - Received a SYN-ACK, and send an ACK back
		if tcpHdr.Flags == (header.TCPFlagSyn|header.TCPFlagAck) && tcpConn.State == "SYN_SENT" {
			// TODO below
			// if (tcpConn.SendBuf.UNA < int32(tcpHdr.AckNum) && int32(tcpHdr.AckNum) <= tcpConn.SendBuf.NXT + 1) {
				// valid ack num

				// if ( tcpConn.SendBuf.UNA > int32(tcpConn.ISN) ) {
				// TODO is this necessary^^? RFC 3.10.7.3
					// updating ACK #
					tcpConn.ACK = tcpHdr.SeqNum + 1

					// sending ACK back
					err := tcpConn.sendTCP([]byte{}, uint32(header.TCPFlagAck), tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
					if err != nil {
						fmt.Println("Could not sent ACK back")
						fmt.Println(err)
						return
					}

					// state is now established
					tcpConn.State = "ESTABLISHED"

					// start thread to send segments (if any) in send buf
					go tcpConn.SendSegment()
					// start thread to read from rec buf (if anything exists)
					go tcpConn.WatchRecvBuf()
				// }
			// }

			// Handshake - received an ACK, so set state to ESTABLISHED
		} else if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "SYN_RECEIVED" {
			tcpConn.State = "ESTABLISHED"

			// signal VAccept to return
			listenConn.ConnCreated <- tcpConn

			// start thread to send segments (if any) in send buf
			go tcpConn.SendSegment()
			// start thread to read from rec buf (if anything exists)
			go tcpConn.WatchRecvBuf()

			// Finished handshake, now receiving actual data and/or ACKs
		} else if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "ESTABLISHED" {
			// tcpConn.AckReceived <- tcpHdr.AckNum
			
			if (len(tcpPayload) > 0) {
				// Calculate remaining space in buffer
				remainingSpace := BUFFER_SIZE - iptcp_utils.CalculateOccupiedRecvBufSpace(tcpConn.RecvBuf.LBR, tcpConn.RecvBuf.NXT)
				if len(tcpPayload) > int(remainingSpace) {
					// TODO
					fmt.Println("Data received is larger than remaining space in receive buffer")
				} else {
					// Copy data into receive buffer
					startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
					for i := 0; i < len(tcpPayload); i++ {
						tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = tcpPayload[i]
					}
					tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))

					// Send an ACK back
					if len(tcpPayload) > 0 {
						tcpConn.CurWindow -= uint16(len(tcpPayload))
						fmt.Println("in handler, sending ack back")
						tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpHdr.SeqNum+uint32(len(tcpPayload)), tcpConn.CurWindow)
					}
					// Send signal that bytes are now in receive buffer
					// tcpConn.RecvSpaceOpen <- true
				}
			}

			tcpStack.HandleACK(packet, tcpHdr, tcpConn)
		} 
		return

	} else if listen_exists {
		// Handshake - received SYN
		if tcpHdr.Flags == header.TCPFlagSyn {
			// Create new normal socket + its send/rec bufs
			// Add new normal socket to tcp stack's table
			newTcpConn := tcpStack.CreateNewNormalConn(tcpHdr, ipHdr)

			// Send a SYN-ACK back to client
			flags := header.TCPFlagSyn | header.TCPFlagAck
			newTcpConn.SeqNum = uint32(newTcpConn.SeqNum) + 1
			err := newTcpConn.sendTCP([]byte{}, uint32(flags), uint32(newTcpConn.SeqNum), newTcpConn.ACK, newTcpConn.CurWindow)
			if err != nil {
				fmt.Println("Error - Could not send SYN-ACK back")
				fmt.Println(err)
				return
			}
			newTcpConn.SeqNum += 1
		}
	} else {
		// drop packet
		return
	}
}

func (tcpStack *TCPStack) HandleACK(packet *IPPacket, header header.TCPFields, tcpConn *TCPConn) {
	ACK := (header.AckNum - tcpConn.ISN)

	if (tcpConn.SendBuf.UNA < int32(ACK) && int32(ACK) <= tcpConn.SendBuf.NXT + 1) {
		// valid ack number, RFC 3.4
		// tcpConn.ACK = header.SeqNum
		tcpConn.SendBuf.UNA = int32(ACK - 1)

	} else {
		// invalid ack number
		return
	}

}

func (tcpStack *TCPStack) CreateNewNormalConn (tcpHdr header.TCPFields, ipHdr ipv4header.IPv4Header) *TCPConn {
	// Create new normal socket + its send/rec bufs
	// add new normal socket to tcp stack's table
	seqNum := int(rand.Uint32())
	SendBuf := &TCPSendBuf{
		Buffer:  make([]byte, BUFFER_SIZE),
		UNA:     0,
		NXT:     0,
		LBW:     -1,
		Channel: make(chan bool), // TODO subject to change
	}
	RecvBuf := &TCPRecvBuf{
		Buffer: make([]byte, BUFFER_SIZE),
		NXT:    0,
		LBR:    -1,
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
		CurWindow:         BUFFER_SIZE,
		ACK:							 tcpHdr.SeqNum + 1,
	}

	// Add the new normal socket to tcp stack's connections table
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

	return tcpConn

}