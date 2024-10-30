package tcp_protocol

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"
	protocol "tcp-tcp-team-pa/pkg"

	"github.com/google/netstack/tcpip/header"
)

func (tcpStack *TCPStack) Initialize(localIP netip.Addr, ipStack *protocol.IPStack) {
	tcpStack.IPStack = *ipStack
	tcpStack.IP = localIP
	tcpStack.ListenTable = make(map[uint16]*TCPListener)
	tcpStack.ConnectionsTable = make(map[uint16]*TCPConn)
	tcpStack.NextSocketID = 0
}

func (stack *TCPStack) Listen(port *net.UDPAddr) (*TCPListener, error) {
	// Create TCPListener struct
	var empty_addr netip.Addr
	tcpListener := &TCPListener{
		ID:         stack.NextSocketID,
		State:      LISTEN,
		LocalPort:  port,
		LocalAddr:  empty_addr,
		RemotePort: nil,
		RemoteAddr: empty_addr,
	}

	// Edit the stack's listen table
	stack.ListenTable[stack.NextSocketID] = tcpListener
	stack.NextSocketID++
	return tcpListener, nil
}

func (stack *TCPStack) Connect(remoteAddr netip.Addr, remotePort uint16) (*TCPConn, error) {
	// Initiate a connection (created a "normal socket")
	min := uint16(20000)
	max := uint16(65535 - 20000)

	// Generate a random port number
	randomNum := min + uint16(rand.Intn(int(max)))

	// Select random 32-bit integer for sequence number
	seqNum := rand.Uint32()

	tcpConnection := &TCPConn{
		ID:         stack.NextSocketID,
		State:      SYN_SENT,
		LocalPort:  randomNum,
		LocalAddr:  stack.IP,
		RemotePort: remotePort,
		RemoteAddr: remoteAddr,
		TCPStack:   stack,
		SeqNum:     seqNum,
	}
	stack.NextSocketID++
	stack.ConnectionsTable[tcpConnection.ID] = tcpConnection

	// Send SYN packet
	err := tcpConnection.sendTCP([]byte{}, header.TCPFlagSyn, int(tcpConnection.SeqNum), 0)
	if err != nil {
		fmt.Println("Could not sent SYN packet")
		return nil, err
	}
	return tcpConnection, nil
}

func (tcpListener TCPListener) Accept() (*TCPConn, error) {
	if tcpListener.State != LISTEN {
		return nil, errors.New("Listener not listening")
	}
	// Read in a SYN packet
	nodeInterface := tcpListener.TCPStack.IPStack.Interfaces
	tcpListener.TCPStack.IPStack.Receive()
	
	// tcpListener maps this connection to the open listen socket

	// Creates a new normal socket


	// Send a SYN-ACK

}

	// Blocks until somebody connects and the socket is in the ESTABLISHED state
	// Once a packet is received, we create a new normal socket between the remote host
	// and our current node.
	// We need to modify our current node's table because we created a new socket in the SYN_RECEIVED STATE
	// We then send a SYN ACK back to the client
	// When the client receives this SYN_ACK, it consults its socket table and sees that there is already a match in the table. It then checks the state of this existing socket and sees that it is in SYN_SENT and we just received a SYN_ACK, so now we can set the state of that socket to ESTABLISHED
	// The client also sends back an ACK

	// At the very end, we want to return the normal socket, which should be in the state ESTABLISHED
}

func (tcpConn *TCPConn) sendTCP(data []byte, flags uint8, seqNum int, ackNum int) error {
	tcpHeader := header.TCPFields{
		SrcPort:       tcpConn.LocalPort,
		DstPort:       tcpConn.RemotePort,
		SeqNum:        uint32(seqNum),
		AckNum:        uint32(ackNum),
		DataOffset:    20,
		Flags:         flags,
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
	tcpConn.TCPStack.IPStack.SendIP(&tcpConn.LocalAddr, 16, tcpConn.RemoteAddr, 6, ipPacketPayload)
}
