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

		// Received a SYN-ACK, and send an ACK back
		if tcpHdr.Flags == (header.TCPFlagSyn|header.TCPFlagAck) && tcpConn.State == "SYN_SENT" {
			flags := header.TCPFlagAck
			tcpConn.ACK = tcpHdr.SeqNum + 1
			err := tcpConn.sendTCP([]byte{}, uint32(flags), tcpHdr.AckNum, tcpHdr.SeqNum+1, tcpConn.CurWindow)

			if err != nil {
				fmt.Println("Could not sent ACK back")
				return
			}
			tcpConn.State = "ESTABLISHED"
			go tcpConn.SendSegment()
			go tcpConn.WatchRecvBuf()

			// Received an ACK, so set state to ESTABLISHED
		} else if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "SYN_RECEIVED" {
			tcpConn.State = "ESTABLISHED"
			listenConn.ConnCreated <- tcpConn
			go tcpConn.SendSegment()
			go tcpConn.WatchRecvBuf()

			// Finished handshake, received actual data
		} else if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "ESTABLISHED" && len(tcpPayload) > 0{
			// tcpConn.AckReceived <- tcpHdr.AckNum
			// Calculate remaining space in buffer
			remainingSpace := iptcp_utils.CalculateRemainingRecvBufSpace(tcpConn.RecvBuf.LBR, tcpConn.RecvBuf.NXT)
			if len(tcpPayload) > int(remainingSpace) {
				fmt.Println("Data received is larger than remaining space in receive buffer")
			} else {
				// Copy data into receive buffer
				startIdx := int(tcpConn.RecvBuf.NXT) % BUFFER_SIZE
				for i := 0; i < len(tcpPayload); i++ {
					tcpConn.RecvBuf.Buffer[(startIdx+i)%BUFFER_SIZE] = tcpPayload[i]
				}
				tcpConn.RecvBuf.NXT += uint32(len(tcpPayload))
				fmt.Println("HERE IS THE RECEIVE NXT IN TCPHANDLER")
				fmt.Println(tcpConn.RecvBuf.NXT)

				// Send an ACK back
				if len(tcpPayload) > 0 {
					tcpConn.CurWindow -= uint16(len(tcpPayload))
					tcpConn.sendTCP([]byte{}, header.TCPFlagAck, tcpConn.SeqNum, tcpHdr.SeqNum+uint32(len(tcpPayload)), tcpConn.CurWindow)
				}
				// Send signal that bytes are now in receive buffer
				// tcpConn.RecvSpaceOpen <- true
			}
		
		
			// receiving ack from sent data
		} else if tcpHdr.Flags == header.TCPFlagAck && tcpConn.State == "ESTABLISHED" && len(tcpPayload) == 0{
			fmt.Println("RECEIVED ACK")
			// tcpStack.HandleACK(packet, tcpHdr, tcpConn)

		}
		return

	} else if listen_exists {
		if tcpHdr.Flags != header.TCPFlagSyn {
			// drop packet because only syn flag should be set and other flags are set
			return
		}

		// Receive SYN in handshake from client

		// Create new normal socket
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

		// Send a SYN-ACK back to client
		flags := header.TCPFlagSyn | header.TCPFlagAck
		tcpConn.SeqNum = uint32(seqNum) + 1
		tcpConn.ACK = tcpHdr.SeqNum + 1
		err := tcpConn.sendTCP([]byte{}, uint32(flags), uint32(seqNum), tcpHdr.SeqNum+1, tcpConn.CurWindow)
		if err != nil {
			fmt.Println("Error - Could not send SYN-ACK back")
			return
		}
	} else {
		// drop packet
		return
	}
}

func (tcpStack *TCPStack) HandleACK(packet *IPPacket, header header.TCPFields, tcpConn *TCPConn) {
	ACK := (header.AckNum - tcpConn.ISN)

	fmt.Println("In handle ack")
	fmt.Println("nxt = ")
	fmt.Println(tcpConn.SendBuf.NXT)

	if (tcpConn.SendBuf.UNA < int32(ACK) && int32(ACK) <= tcpConn.SendBuf.NXT + 1) {
		// valid ack number, RFC 3.4
		tcpConn.ACK = header.AckNum
		tcpConn.SendBuf.UNA = int32(ACK - 1)
		fmt.Println("New una = ")
		fmt.Println(tcpConn.SendBuf.UNA)

	} else {
		// invalid ack number
		return
	}

}
