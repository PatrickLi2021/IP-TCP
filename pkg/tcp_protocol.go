package protocol

import (
	"fmt"
	"math/rand"
	"net/netip"
	"strconv"
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
	PacketData 		[]byte
}

type Retransmits struct {
	RTOTimer *time.Ticker
	SRTT     time.Duration
	Alpha    float32
	Beta     float32
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
	EarlyArrivals			map[uint32]*EarlyArrivalPacket
	RTStruct					*Retransmits

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
				tcpConn.State = "FIN_WAIT_2"
			}
		case "FIN_WAIT_2":
			if tcpHdr.Flags == header.TCPFlagFin|header.TCPFlagAck {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, len(tcpPayload))
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
			if tcpHdr.Flags == header.TCPFlagAck | header.TCPFlagFin && len(tcpPayload) == 0 {
				tcpConn.handleReceivedData(tcpPayload, tcpHdr)
				tcpStack.HandleACK(packet, tcpHdr, tcpConn, 0)
			}
		case "LAST_ACK":
			if tcpHdr.Flags == header.TCPFlagAck && len(tcpPayload) == 0 {
				tcpConn.State = "CLOSED"
				delete(tcpStack.ConnectionsTable, fourTuple)
				fmt.Println("This socket is closed")
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

	// creating RTStruct
	RTStruct := &Retransmits{
		SRTT:    -1 * time.Second,
		Alpha:   0.85,
		Beta:    1.5,
		RTQueue: []*RTPacket{},
		RTO:     RTO_MIN,
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
		SeqNum:            uint32(seqNum),
		ISN:               0, //uint32(seqNum)
		SendBuf:           SendBuf,
		RecvBuf:           RecvBuf,
		SendSpaceOpen:     make(chan bool, 1),
		RecvBufferHasData: make(chan bool, 1),
		SfRfEstablished:   make(chan bool),
		SendBufferHasData: make(chan bool, 1),
		CurWindow:         BUFFER_SIZE,
		ACK:               tcpHdr.SeqNum + 1,
		ReceiverWin:       BUFFER_SIZE,
		EarlyArrivals: 		 make(map[uint32]*EarlyArrivalPacket),
		RTStruct:					 RTStruct,
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
	if len(tcpPayload) > 0 || (tcpConn.State == "CLOSE_WAIT" && len(tcpPayload) == 0) || (tcpConn.State == "FIN_WAIT_2" && len(tcpPayload) == 0){
		// // Calculate remaining space in buffer
		// remainingSpace := BUFFER_SIZE - tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()

		// Zero Window Probe case
		if (uint16(len(tcpPayload)) > tcpConn.CurWindow) {
			// don't read data in, until 
			// send ack back, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			return
		} else if (tcpHdr.SeqNum < tcpConn.ACK) {
			// send ack back, duplicate ack likely, don't increment anything
			tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
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
		} else {
			// SEQ NUM = ACK NUM
			// Copy data into receive buffer
			startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
			for i := 0; i < len(tcpPayload); i++ {
				tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = tcpPayload[i]
			}
			tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))
			tcpConn.CurWindow -= uint16(len(tcpPayload))
			tcpConn.ACK += uint32(len(tcpPayload)) 

			if (len(tcpPayload) == 0) {
				// receiving FIN + ACK which has len 0
				tcpConn.RecvBuf.NXT += 1
				tcpConn.CurWindow -= 1
				tcpConn.ACK += 1
			}

			// EARLY ARRIVALS
			// Check the early arrivals queue to see if we can reconstruct the data
			for {
				earliestPacket, ok := tcpConn.EarlyArrivals[tcpConn.ACK]
				if (!ok) {
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

				if (len(earliestPacket.PacketData) == 0) {
					// FIN + ACK is in early arrival queue
					tcpConn.RecvBuf.NXT += 1
					tcpConn.CurWindow -= 1
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

			// Send signal that bytes are now in receive buffer
			// tcpConn.RecvSpaceOpen <- true
			// }
		}
	}
}

func (tcpStack *TCPStack) HandleACK(packet *IPPacket, header header.TCPFields, tcpConn *TCPConn, payloadLen int) {
	// moving UNA since we have ACKed some packets
	ACK := (header.AckNum - tcpConn.ISN)

	// TODO:
	tcpConn.ReceiverWin = uint32(header.WindowSize)

	if (payloadLen > int(tcpConn.CurWindow)) {
		// if received zero window probe, don't update UNA
		return
	}

	fmt.Println("in handle ACK")
	fmt.Println(int32(ACK))
	fmt.Println(tcpConn.SendBuf.UNA + 1)
	fmt.Println(tcpConn.SendBuf.NXT + 1)
	if tcpConn.SendBuf.UNA + 1 < int32(ACK) && int32(ACK) <= tcpConn.SendBuf.NXT+1 {
		// valid ack number, RFC 3.4
		// tcpConn.ACK = header.SeqNum
		tcpConn.SendBuf.UNA = int32(ACK-1)
		if len(tcpConn.SendSpaceOpen) == 0 {
			tcpConn.SendSpaceOpen <- true
		}
		fmt.Println("gonna call handleretransmission")
		fmt.Println(tcpConn.State)
		tcpConn.RTStruct.handleRetransmission(header.AckNum)

	} else {
		// invalid ack number, maybe duplicate
		return
	}

}

func (rtStruct *Retransmits) handleRetransmission(ackNum uint32) {
	fmt.Println("BEGIN, queue len: " + strconv.Itoa(len(rtStruct.RTQueue)))
	// removing packets to retransmit based on new ACK
	splitIndex := -1 //represents the last packet that can be removed from RTQueue
	for index, packet := range rtStruct.RTQueue {
		if packet != nil {
			if packet.SeqNum < ackNum {
				splitIndex = index
			} else {
				break
			}
		}
	}
	// This means that there is something to be removed from the retransmission queue
	if splitIndex >= 0 {

		if rtStruct.RTQueue[splitIndex].NumTries == 0 {
			// only do this if ack is NOT for retransmitted segment
			// Calculate RTT and SRTT
			recentPacketTime := rtStruct.RTQueue[splitIndex].Timestamp
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
	fmt.Println("END, queue len: " + strconv.Itoa(len(rtStruct.RTQueue)))
}