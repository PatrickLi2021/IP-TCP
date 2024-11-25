package protocol

import (
	"fmt"
	"math/rand"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"
	"time"

	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"github.com/google/netstack/tcpip/header"
)

const (
	RTO_MIN     = 100 * time.Millisecond
	RTO_MAX     = 5 * time.Second
	MAX_RETRIES = 3
)

type FourTuple struct {
	remotePort uint16
	remoteAddr netip.Addr
	srcPort    uint16
	srcAddr    netip.Addr
}

type EarlyArrivalPacket struct {
	PacketData []byte
}

type Retransmits struct {
	RTOTimer *time.Ticker
	SRTT     time.Duration
	Alpha    float32
	Beta     float32
	// RTQueueLock sync.RWMutex
	RTQueue  []*RTPacket
	RTO      time.Duration
}

type RTPacket struct {
	Timestamp time.Time
	SeqNum    uint32
	AckNum    uint32
	Data      []byte
	Flags     uint32
	NumTries  uint32
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
	EarlyArrivals     map[uint32]*EarlyArrivalPacket
	RTStruct          *Retransmits
	OtherSideLastSeq  uint32
	IsClosing					bool
	OtherSideISN			uint32

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
				// updating ACK #
				tcpConn.ACK = tcpHdr.SeqNum + 1

				tcpConn.OtherSideISN = tcpHdr.SeqNum

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
			} else if tcpHdr.Flags == header.TCPFlagAck | header.TCPFlagFin {
				// Ensure that this receiver has received all the data from the sender
				// (i.e. tcpHdr.AckNum == uint32(tcpConn.RecvBuf.NXT))
				tcpConn.OtherSideLastSeq = tcpHdr.SeqNum
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
				tcpConn.State = "CLOSE_WAIT"
			}
		case "FIN_WAIT_1":
			if tcpHdr.Flags == header.TCPFlagAck {
					if tcpHdr.AckNum == tcpConn.SeqNum {
						// got the ack back for the FIN ACK, can go into FIN WAIT 2
						tcpConn.State = "FIN_WAIT_2"
					}
					// ACK for actual data
					tcpConn.handleReceivedData(tcpPayload, tcpHdr)
					tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}
		// Other party has initiated close, but we can still receive data (and should send ACKs back)
		case "CLOSE_WAIT":
			if tcpHdr.Flags == header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}
		case "FIN_WAIT_2":
			if tcpHdr.Flags == header.TCPFlagFin | header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
				tcpConn.State = "TIME_WAIT"
				time.Sleep(5 * time.Second)
				tcpConn.State = "CLOSED"
				delete(tcpStack.ConnectionsTable, fourTuple)
			} else if tcpHdr.Flags == header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}
		case "TIME_WAIT":
			if (tcpHdr.Flags == header.TCPFlagAck) || (tcpHdr.Flags == header.TCPFlagAck | header.TCPFlagFin) {
				// receiving normal data
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
			}

		case "LAST_ACK":
			if tcpHdr.Flags == header.TCPFlagAck && len(tcpPayload) == 0 {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))

				if (tcpHdr.AckNum == tcpConn.SeqNum) {
					// ack back for FIN ACK
					tcpConn.State = "CLOSED"
					delete(tcpStack.ConnectionsTable, fourTuple)
					fmt.Println("This socket is closed")
				}
			}
		}
		return

	} else if listen_exists {
		// Handshake - received SYN
		if tcpHdr.Flags == header.TCPFlagSyn {
			// Create new normal socket + its send/rec bufs
			// Add new normal socket to tcp stack's table
			newTcpConn := tcpStack.CreateNewNormalConn(tcpHdr, ipHdr)
			newTcpConn.OtherSideISN = tcpHdr.SeqNum

			// Send a SYN-ACK back to client
			flags := header.TCPFlagSyn | header.TCPFlagAck
			// newTcpConn.SeqNum += 1
			err := newTcpConn.sendTCP([]byte{}, uint32(flags), newTcpConn.SeqNum, newTcpConn.ACK, newTcpConn.CurWindow)
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

func (tcpStack *TCPStack) CreateNewNormalConn(tcpHdr header.TCPFields, ipHdr ipv4header.IPv4Header) *TCPConn {
	// Create new normal socket + its send/rec bufs
	// add new normal socket to tcp stack's table
	seqNum := rand.Uint32()
	SendBuf := &TCPSendBuf{
		Buffer:  make([]byte, BUFFER_SIZE),
		UNA:     0,
		NXT:     0,
		LBW:     -1,
	}
	RecvBuf := &TCPRecvBuf{
		Buffer:   make([]byte, BUFFER_SIZE),
		NXT:      0,
		LBR:      -1,
	}

	// creating RTStruct
	RTStruct := &Retransmits{
		SRTT:     -1 * time.Second,
		Alpha:    0.85,
		Beta:     1.5,
		RTQueue:  []*RTPacket{},
		RTO:      RTO_MIN,
		RTOTimer: time.NewTicker(RTO_MIN),
	}

	tcpConn := &TCPConn{
		ID:                tcpStack.NextSocketID,
		State:             "SYN_RECEIVED",
		LocalPort:         tcpHdr.DstPort,
		LocalAddr:         tcpStack.IP,
		RemotePort:        tcpHdr.SrcPort,
		RemoteAddr:        ipHdr.Src,
		TCPStack:          tcpStack,
		SeqNum:            seqNum,
		ISN:               seqNum,
		SendBuf:           SendBuf,
		RecvBuf:           RecvBuf,
		SendSpaceOpen:     make(chan bool, 1),
		RecvBufferHasData: make(chan bool, 1),
		SfRfEstablished:   make(chan bool),
		SendBufferHasData: make(chan bool, 1),
		CurWindow:         BUFFER_SIZE,
		ACK:               tcpHdr.SeqNum + 1,
		ReceiverWin:       BUFFER_SIZE,
		EarlyArrivals:     make(map[uint32]*EarlyArrivalPacket),
		RTStruct:          RTStruct,
		OtherSideLastSeq:  0,
		IsClosing: 				 false,
		OtherSideISN: 		 0,
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
	if len(tcpPayload) > 0 || 
	(tcpConn.State == "ESTABLISHED" && tcpHdr.Flags == header.TCPFlagAck | header.TCPFlagFin) ||
	(tcpConn.State == "FIN_WAIT_2" && tcpHdr.Flags == header.TCPFlagAck | header.TCPFlagFin) {
		// Zero Window Probe case
		if uint16(len(tcpPayload)) > tcpConn.CurWindow {
			// don't read data in, until
			// send ack back, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			return
		} else if tcpHdr.SeqNum < tcpConn.ACK {
			// send ack back, duplicate data likely, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			if (tcpConn.OtherSideLastSeq != 0) && (tcpConn.ACK == uint32(tcpConn.OtherSideLastSeq) + 1) {
				tcpConn.IsClosing = true
			}
			return

			// EARLY ARRIVAL
			// Packet is early arrival
		} else if tcpHdr.SeqNum > tcpConn.ACK {
			// Create early arrival packet
			earlyArrivalPacket := &EarlyArrivalPacket{
				PacketData: tcpPayload,
			}
			tcpConn.EarlyArrivals[tcpHdr.SeqNum] = earlyArrivalPacket
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			if (tcpConn.OtherSideLastSeq != 0) && (tcpConn.ACK == uint32(tcpConn.OtherSideLastSeq) + 1) {
				tcpConn.IsClosing = true
			}
		} else {
			// SEQ NUM = ACK NUM
			// Copy data into receive buffer
			if len(tcpPayload) > 0 {
				startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
				for i := 0; i < len(tcpPayload); i++ {
					tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = tcpPayload[i]
				}
				tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))
				tcpConn.CurWindow -= uint16(len(tcpPayload))
				tcpConn.ACK += uint32(len(tcpPayload))
			}

			if tcpHdr.Flags == (header.TCPFlagAck | header.TCPFlagFin) {
				// receiving FIN + ACK which has len 0
				// tcpConn.RecvBuf.FIN = int32(tcpConn.RecvBuf.NXT)
				// tcpConn.RecvBuf.NXT += 1
				tcpConn.ACK += 1
			}

			// EARLY ARRIVALS
			// Check the early arrivals queue to see if we can reconstruct the data
			for {
				earliestPacket, ok := tcpConn.EarlyArrivals[tcpConn.ACK]
				if !ok {
					// no early arrivals can be added to recv buf yet, still missing data
					break
				}
				// Fill receive buffer with IN-ORDER packets
				packetSeqNum := tcpConn.ACK
				packetLen := len(earliestPacket.PacketData)
				startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
				for i := 0; i < packetLen; i++ {
					tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = earliestPacket.PacketData[i]
				}
				tcpConn.RecvBuf.NXT += uint32(packetLen)
				tcpConn.CurWindow -= uint16(packetLen)
				tcpConn.ACK += uint32(packetLen)

				if len(earliestPacket.PacketData) == 0 {
					// FIN + ACK is in early arrival queue
					// tcpConn.RecvBuf.NXT += 1
					tcpConn.ACK += 1
				}
				// Remove early arrival from map since we've copied it into receive buffer
				delete(tcpConn.EarlyArrivals, packetSeqNum)
			}

			if len(tcpConn.RecvBufferHasData) == 0 {
				tcpConn.RecvBufferHasData <- true
			}
			// Send a normal ACK back
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			if (tcpConn.OtherSideLastSeq != 0) && (tcpConn.ACK == uint32(tcpConn.OtherSideLastSeq) + 1) {
				tcpConn.IsClosing = true
			}
		}
	} 
	
}

func (tcpStack *TCPStack) HandleACK(packet *IPPacket, header header.TCPFields, tcpConn *TCPConn, payloadLen int) {
	// moving UNA since we have ACKed some packets
	ACK := (header.AckNum - tcpConn.ISN)

	tcpConn.ReceiverWin = uint32(header.WindowSize)

	tcpConn.RTStruct.handleRetransmission(header.AckNum)

	if uint16(payloadLen) > tcpConn.CurWindow {
		// if received zero window probe, don't update UNA
		return
	}
	fmt.Println("IN HANDLE ACK")
	if uint32(tcpConn.SendBuf.UNA+1) < ACK && ACK <= uint32(tcpConn.SendBuf.NXT+1) {
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

func (rtStruct *Retransmits) handleRetransmission(ackNum uint32) {
	// removing packets to retransmit based on new ACK
	splitIndex := -1 //represents the last packet that can be removed from RTQueue
	ackPacketIndex := -1
	for index, packet := range rtStruct.RTQueue {
		if packet != nil {
			if packet.SeqNum < ackNum {
				splitIndex = index
			} else if packet.SeqNum + uint32(len(packet.Data)) == ackNum {
				ackPacketIndex = index
			} else {
				break
			}
		}
	}

	// recalc RTO if necessary
	if (ackPacketIndex >= 0) {
		if rtStruct.RTQueue[ackPacketIndex].NumTries == 0 {
			// only do this if ack is NOT for retransmitted segment
			// Calculate RTT and SRTT
			recentPacketTime := rtStruct.RTQueue[ackPacketIndex].Timestamp
			rtt := time.Since(recentPacketTime)
	
			if rtStruct.SRTT == -1*time.Second {
				// Following RFC guidelines
				rtStruct.SRTT = rtt
			} else {
				rtStruct.SRTT = time.Duration(1-rtStruct.Alpha)*rtStruct.SRTT + time.Duration(rtStruct.Alpha)*rtt
			}
			// Calculate RTO
			rtStruct.RTO = max(RTO_MIN, min(time.Duration(rtStruct.Beta)*rtStruct.SRTT), RTO_MAX)
			// Round up RTO if it's less than 1 second
			rtStruct.RTO = max(1*time.Second, rtStruct.RTO)
	
			// Restart timer with re-calculated RTO
			rtStruct.RTOTimer.Reset(rtStruct.RTO)
		}
	}

	// This means that there is something to be removed from the retransmission queue
	if splitIndex >= 0 {
		// packets to be removed from RTQueue
		if splitIndex+1 == len(rtStruct.RTQueue) {

			rtStruct.RTQueue = []*RTPacket{}
		} else {
			rtStruct.RTQueue = rtStruct.RTQueue[splitIndex+1:]
		}
	}
	// If retransmission queue is empty, turn off RTO timer (all outstanding data has been ACKed)
	if len(rtStruct.RTQueue) == 0 {
		rtStruct.RTOTimer.Stop()
	}
}
