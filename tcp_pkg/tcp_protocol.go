package tcp_protocol

import (
	"errors"
	"fmt"
	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"net/netip"
	"tcp-tcp-team-pa/iptcp_utils"
	protocol "tcp-tcp-team-pa/pkg"
)

type TCPState int

const (
	MaxMessageSize = 1400
)

const (
	CLOSED TCPState = iota
	LISTEN
	SYN_SENT
	SYN_RECEIVED
	ESTABLISHED
	FIN_WAIT_1
	FIN_WAIT_2
	CLOSING
	TIME_WAIT
	CLOSE_WAIT
	LAST_ACK
)

type TCPListener struct {
	ID         uint16
	State      TCPState
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
}

type TCPConn struct {
	ID         uint16
	State      TCPState
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
	SeqNum     uint32
}

type TCPStack struct {
	ListenTable      map[uint16]*TCPListener
	ConnectionsTable map[uint16]*TCPConn
	IP               netip.Addr
	NextSocketID     uint16 // unique ID for all sockets per node
	IPStack          protocol.IPStack
}

type FourTuple struct {
	remotePort uint16
	remoteAddr netip.Addr
	srcPort    uint16
	srcAddr    netip.Addr
}

func (tcpStack *TCPStack) TCPHandler(packet *protocol.IPPacket) error {
	// Parse TCP header
	ipHdr := packet.Header
	tcpHeaderAndData := packet.Payload

	// Parse TCP header into a struct and get the payload
	tcpHdr := iptcp_utils.ParseTCPHeader(tcpHeaderAndData)
	tcpPayload := tcpHeaderAndData[tcpHdr.DataOffset:]

	tcpChecksumFromHeader := tcpHdr.Checksum
	tcpHdr.Checksum = 0
	tcpComputedChecksum := iptcp_utils.ComputeTCPChecksum(&tcpHdr, ipHdr.Src, ipHdr.Dst, tcpPayload)

	if tcpComputedChecksum != tcpChecksumFromHeader {
		return errors.New("Checksum is not correct")
	}

	// Get the port and flags
	fourTuple := &FourTuple{
		remotePort: tcpHdr.DstPort,
		remoteAddr: ipHdr.Dst,
		srcPort:    tcpHdr.SrcPort,
		srcAddr:    ipHdr.Src,
	}

	// Check if TCP header flag is SYN
	if tcpHdr.Flags == 1 {
		value, exists := tcpStack.ListenTable[fourTuple]
		if !exists {
			errors.New("Associated listener not found on this node")
		}

	}
}
