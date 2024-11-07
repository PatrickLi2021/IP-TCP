package protocol

import (
	"fmt"
	"math/rand"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

func (stack *TCPStack) VListen(port uint16) (*TCPListener, error) {
	// Create TCPListener struct
	emptyAddr, _ := netip.ParseAddr("0.0.0.0")
	tcpListener := &TCPListener{
		ID:         stack.NextSocketID,
		State:      "LISTEN",
		LocalPort:  port,
		LocalAddr:  emptyAddr,
		RemotePort: 0,
		RemoteAddr: emptyAddr,
		Channel:    make(chan *TCPConn),
	}

	// Edit the stack's listen table
	stack.ListenTable[port] = tcpListener
	stack.NextSocketID++
	return tcpListener, nil
}

func (stack *TCPStack) VConnect(remoteAddr netip.Addr, remotePort uint16) (*TCPConn, error) {
	// Initiate a connection (created a "normal socket")

	// Generate a random port number
	min := uint16(20000)
	max := uint16(65535 - 20000)
	randomNum := min + uint16(rand.Intn(int(max)))

	// Select random 32-bit integer for sequence number
	seqNum := rand.Uint32()

	// creating send buf
	SendBuf := &TCPSendBuffer{
		Buffer: make([]byte, BUFFER_SIZE),
		UNA: 0,
		NXT: 0,
		LBW: 0,
		Channel: make(chan bool), // TODO subject to change
	}
	tcpConnection := &TCPConn{
		ID:         stack.NextSocketID,
		State:      "SYN_SENT",
		LocalPort:  randomNum,
		LocalAddr:  stack.IP,
		RemotePort: remotePort,
		RemoteAddr: remoteAddr,
		TCPStack:   stack,
		SeqNum:     seqNum,
		ISN: 				seqNum,
		SendBuf:    SendBuf,
	}

	fourTuple := FourTuple{
		remotePort: remotePort,
		remoteAddr: remoteAddr,
		srcPort:    randomNum,
		srcAddr:    stack.IP,
	}

	stack.NextSocketID++
	stack.ConnectionsTable[fourTuple] = tcpConnection

	// Send SYN packet
	err := tcpConnection.sendTCP([]byte{}, header.TCPFlagSyn, uint32(tcpConnection.SeqNum), 0)
	if err != nil {
		fmt.Println("Could not sent SYN packet")
		return nil, err
	}
	tcpConnection.SeqNum += 1
	return tcpConnection, nil
}

func (tcpListener *TCPListener) VAccept() (*TCPConn, error) {
	tcpConn := <-tcpListener.Channel
	return tcpConn, nil
}

func (tcpConn *TCPConn) sendTCP(data []byte, flags uint32, seqNum uint32, ackNum uint32) error {
	tcpHeader := header.TCPFields{
		SrcPort:       tcpConn.LocalPort,
		DstPort:       tcpConn.RemotePort,
		SeqNum:        uint32(seqNum),
		AckNum:        uint32(ackNum),
		DataOffset:    20,
		Flags:         uint8(flags),
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}
	checksum := iptcp_utils.ComputeTCPChecksum(&tcpHeader, tcpConn.LocalAddr, tcpConn.RemoteAddr, data)
	tcpHeader.Checksum = checksum
	tcpHeaderBytes := make(header.TCP, iptcp_utils.TcpHeaderLen)
	tcpHeaderBytes.Encode(&tcpHeader)

	// Combine the TCP header + payload into one byte array, which
	// becomes the payload of the IP packet
	ipPacketPayload := make([]byte, 0, len(tcpHeaderBytes)+len(data))
	ipPacketPayload = append(ipPacketPayload, tcpHeaderBytes...)
	ipPacketPayload = append(ipPacketPayload, []byte(data)...)
	tcpConn.TCPStack.IPStack.SendIP(&tcpConn.LocalAddr, 32, tcpConn.RemoteAddr, 6, ipPacketPayload)
	return nil
}
