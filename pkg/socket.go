package protocol

import (
	"fmt"
	"math/rand"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"
	"time"

	"github.com/google/netstack/tcpip/header"
)

func (stack *TCPStack) VListen(port uint16) (*TCPListener, error) {
	// Create TCPListener struct
	emptyAddr, _ := netip.ParseAddr("0.0.0.0")
	tcpListener := &TCPListener{
		ID:          stack.NextSocketID,
		State:       "LISTEN",
		LocalPort:   port,
		LocalAddr:   emptyAddr,
		RemotePort:  0,
		RemoteAddr:  emptyAddr,
		ConnCreated: make(chan *TCPConn),
	}

	// Edit the stack's listen table
	fourTuple := &FourTuple{
		remotePort: 0,
		remoteAddr: emptyAddr,
		srcPort:    port,
		srcAddr:    emptyAddr,
	}
	stack.SocketIDToConn[uint32(stack.NextSocketID)] = fourTuple
	stack.ListenTable[fourTuple.srcPort] = tcpListener
	stack.NextSocketID++
	return tcpListener, nil
}

func (stack *TCPStack) VConnect(remoteAddr netip.Addr, remotePort uint16) (*TCPConn, error) {
	// Generate a random port number
	min := uint16(20000)
	max := uint16(65535 - 20000)
	randomPort := min + uint16(rand.Intn(int(max)))

	// Select random 32-bit integer for sequence number
	seqNum := rand.Uint32()

	// creating send and receive buffers
	SendBuf := &TCPSendBuf{
		Buffer:  make([]byte, BUFFER_SIZE),
		UNA:     0,
		NXT:     0,
		LBW:     -1,
		Channel: make(chan bool), // TODO subject to change
	}

	RecvBuf := &TCPRecvBuf{
		Buffer:   make([]byte, BUFFER_SIZE),
		LBR:      -1,
		NXT:      0,
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
	// Create new connection
	tcpConn := &TCPConn{
		ID:                stack.NextSocketID,
		State:             "SYN_SENT",
		LocalPort:         randomPort,
		LocalAddr:         stack.IP,
		RemotePort:        remotePort,
		RemoteAddr:        remoteAddr,
		TCPStack:          stack,
		SeqNum:            seqNum,
		ISN:               seqNum,
		SendBuf:           SendBuf,
		RecvBuf:           RecvBuf,
		SfRfEstablished:   make(chan bool),
		SendBufferHasData: make(chan bool, 1),
		RecvBufferHasData: make(chan bool, 1),
		SendSpaceOpen:     make(chan bool, 1),
		CurWindow:         BUFFER_SIZE,
		ReceiverWin:       BUFFER_SIZE,
		EarlyArrivals: 		 make(map[uint32]*EarlyArrivalPacket),
		RTStruct:					 RTStruct,
	}
	fourTuple := &FourTuple{
		remotePort: remotePort,
		remoteAddr: remoteAddr,
		srcPort:    randomPort,
		srcAddr:    stack.IP,
	}
	stack.SocketIDToConn[uint32(stack.NextSocketID)] = fourTuple
	stack.NextSocketID++
	stack.ConnectionsTable[*fourTuple] = tcpConn

	// Send SYN packet
	err := tcpConn.sendTCP([]byte{}, header.TCPFlagSyn, uint32(tcpConn.SeqNum), 0, tcpConn.CurWindow)
	if err != nil {
		fmt.Println("Could not sent SYN packet")
		return nil, err
	}
	tcpConn.SeqNum += 1
	return tcpConn, nil
}

func (tcpListener *TCPListener) VAccept() (*TCPConn, error) {
	tcpConn := <-tcpListener.ConnCreated
	return tcpConn, nil
}

func (tcpConn *TCPConn) sendTCP(data []byte, flags uint32, seqNum uint32, ackNum uint32, windowSize uint16) error {
	tcpHeader := header.TCPFields{
		SrcPort:       tcpConn.LocalPort,
		DstPort:       tcpConn.RemotePort,
		SeqNum:        uint32(seqNum),
		AckNum:        uint32(ackNum),
		DataOffset:    20,
		Flags:         uint8(flags),
		WindowSize:    windowSize,
		Checksum:      0,
		UrgentPointer: 0,
	}
	checksum := iptcp_utils.ComputeTCPChecksum(&tcpHeader, tcpConn.LocalAddr, tcpConn.RemoteAddr, data)
	tcpHeader.Checksum = checksum
	tcpHeaderBytes := make(header.TCP, iptcp_utils.TcpHeaderLen)
	tcpHeaderBytes.Encode(&tcpHeader)

	// Combine the TCP header + payload into one byte array, which becomes the payload of the IP packet
	ipPacketPayload := make([]byte, 0, len(tcpHeaderBytes)+len(data))
	ipPacketPayload = append(ipPacketPayload, tcpHeaderBytes...)
	ipPacketPayload = append(ipPacketPayload, []byte(data)...)
	tcpConn.TCPStack.IPStack.SendIP(&tcpConn.LocalAddr, 32, tcpConn.RemoteAddr, 6, ipPacketPayload)
	return nil
}