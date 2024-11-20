package protocol

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"
	"time"

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
	RecvBufferHasData chan bool
	SendBufferHasData chan bool
	ISN               uint32
	ACK               uint32
	SfRfEstablished   chan bool
	AckReceived       chan uint32
	CurWindow         uint16
	TotalBytesSent    uint32
	ReceiverWin       uint32

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
		switch tcpConn.State {
		case "SYN_SENT":
			if tcpHdr.Flags == (header.TCPFlagSyn | header.TCPFlagAck) {
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
				tcpConn.SfRfEstablished <- true
				// }
				// }

				// Handshake - received an ACK, so set state to ESTABLISHED
			}
		case "SYN_RECEIVED":
			if tcpHdr.Flags == header.TCPFlagAck {
				tcpConn.State = "ESTABLISHED"
				// signal VAccept to return
				listenConn.ConnCreated <- tcpConn
				// Finished handshake, now receiving actual data and/or ACKs
			}
		case "ESTABLISHED":
			if tcpHdr.Flags == header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			} else if header.TCPFlagFin == (header.TCPFlagFin & tcpHdr.Flags) {
				// Ensure that this receiver has received all the data from the sender
				// (i.e. tcpHdr.AckNum == uint32(tcpConn.RecvBuf.NXT))
				if true {
					tcpConn.State = "CLOSE_WAIT"
					// Send an ACK back
					tcpConn.ACK = tcpHdr.SeqNum + 1
					tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
				}
			}
		case "FIN_WAIT_1":
			if tcpHdr.Flags == header.TCPFlagAck {
				// May need to introduce packet sequence and ack checking to make sure everything is in the correct
				// order
				tcpConn.State = "FIN_WAIT_2"
				tcpStack.ConnectionsTable[fourTuple] = tcpConn
			}
		case "FIN_WAIT_2":
			if tcpHdr.Flags == header.TCPFlagFin|header.TCPFlagAck {
				tcpStack.ConnectionsTable[fourTuple] = tcpConn
				tcpConn.ACK = tcpHdr.SeqNum + 1
				tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
				tcpConn.State = "TIME_WAIT"
				time.Sleep(5 * time.Second)
				delete(tcpStack.ConnectionsTable, fourTuple)
				fmt.Println("This socket is closed")
			} else if tcpHdr.Flags == header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}
		// Other party has initiated close, but we can still receive data (and should send ACKs back)
		case "CLOSE_WAIT":
			if tcpHdr.Flags == header.TCPFlagAck && len(tcpPayload) == 0 {
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}
		case "LAST_ACK":
			if tcpHdr.Flags == header.TCPFlagAck && len(tcpPayload) == 0 {
				tcpConn.State = "CLOSED"
				delete(tcpStack.ConnectionsTable, fourTuple)
				fmt.Println("This socket is closed, and was in CLOSE_WAIT")
			}
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

func (tcpStack *TCPStack) HandleACK(packet *IPPacket, header header.TCPFields, tcpConn *TCPConn, payloadLen int) {
	// moving UNA since we have ACKed some packets
	ACK := (header.AckNum - tcpConn.ISN)

	// TODO:
	tcpConn.ReceiverWin = uint32(header.WindowSize)

	if payloadLen > int(tcpConn.CurWindow) {
		// if received zero window probe, don't update UNA
		return
	}

	if tcpConn.SendBuf.UNA+1 < int32(ACK) && int32(ACK) <= tcpConn.SendBuf.NXT+1 {
		// valid ack number, RFC 3.4
		// tcpConn.ACK = header.SeqNum
		tcpConn.SendBuf.UNA = int32(ACK - 1)
		if len(tcpConn.SendSpaceOpen) == 0 {
			tcpConn.SendSpaceOpen <- true
		}

	} else {
		// invalid ack number, maybe duplicate
		return
	}

}

func (tcpStack *TCPStack) CreateNewNormalConn(tcpHdr header.TCPFields, ipHdr ipv4header.IPv4Header) *TCPConn {
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
		Buffer:   make([]byte, BUFFER_SIZE),
		NXT:      0,
		LBR:      -1,
		Waiting:  false,
		ChanSent: false,
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
		SendSpaceOpen:     make(chan bool, 1),
		RecvBufferHasData: make(chan bool, 1),
		SfRfEstablished:   make(chan bool),
		SendBufferHasData: make(chan bool, 1),
		CurWindow:         BUFFER_SIZE,
		ACK:               tcpHdr.SeqNum + 1,
		ReceiverWin:       BUFFER_SIZE,
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

func (tcpConn *TCPConn) handleReceivedData(tcpPayload []byte, tcpHdr header.TCPFields) {
	// tcpConn.AckReceived <- tcpHdr.AckNum
	if len(tcpPayload) > 0 {
		// Calculate remaining space in buffer
		remainingSpace := BUFFER_SIZE - tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()

		// Zero Window Probe case
		if int32(len(tcpPayload)) > remainingSpace {
			// don't read data in, until
			// send ack back, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			return
		} else if tcpHdr.SeqNum < tcpConn.ACK {
			// send ack back, duplicate ack likely, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			return
		}

		if len(tcpConn.RecvBufferHasData) == 0 {
			tcpConn.RecvBufferHasData <- true
		}

		// Copy data into receive buffer
		startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
		for i := 0; i < len(tcpPayload); i++ {
			tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = tcpPayload[i]
		}
		tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))

		if len(tcpConn.RecvBufferHasData) == 0 {
			tcpConn.RecvBufferHasData <- true
		}

		// Send an ACK back
		if len(tcpPayload) > 0 {
			// normal acks back
			tcpConn.CurWindow -= uint16(len(tcpPayload))
			tcpConn.ACK += uint32(len(tcpPayload)) //TODO may need to change
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
		}
		// Send signal that bytes are now in receive buffer
		// tcpConn.RecvSpaceOpen <- true
		// }
	}
}
