package tcp_protocol

import (
	"errors"
	"fmt"
	"github.com/google/netstack/tcpip/header"
	"math/rand/v2"
	"net/netip"
	"strconv"
	"tcp-tcp-team-pa/iptcp_utils"
	protocol "tcp-tcp-team-pa/pkg"
)

type TCPState int

const (
	MaxMessageSize = 1400
)

type TCPListener struct {
	ID         uint16
	State      string
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
	Channel    chan *protocol.IPPacket
}

type TCPConn struct {
	ID         uint16
	State      string
	LocalPort  uint16
	LocalAddr  netip.Addr
	RemotePort uint16
	RemoteAddr netip.Addr
	TCPStack   *TCPStack
	SeqNum     uint32
	Channel    chan *protocol.IPPacket
}

type TCPStack struct {
	ListenTable      map[*FourTuple]*TCPListener
	ConnectionsTable map[*FourTuple]*TCPConn
	IP               netip.Addr
	NextSocketID     uint16 // unique ID for each sockets per node
	IPStack          protocol.IPStack
}

type FourTuple struct {
	remotePort uint16
	remoteAddr netip.Addr
	srcPort    uint16
	srcAddr    netip.Addr
}

func (tcpStack *TCPStack) TCPHandler(packet *protocol.IPPacket) error {
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
		return errors.New("checksum is not correct")
	}

	// Get the port and flags
	fourTuple := &FourTuple{
		remotePort: tcpHdr.DstPort,
		remoteAddr: ipHdr.Dst,
		srcPort:    tcpHdr.SrcPort,
		srcAddr:    ipHdr.Src,
	}

	if tcpHdr.Flags == header.TCPFlagSyn {
		_, exists := tcpStack.ListenTable[fourTuple]
		if !exists {
			return errors.New("associated listener not found on this node")
		}
		// Create new TCPConnection
		seqNum := int(rand.Uint32())
		tcpConn := &TCPConn{
			State:      "SYN_RECEIVED",
			LocalPort:  tcpHdr.DstPort,
			LocalAddr:  tcpStack.IP,
			RemotePort: tcpHdr.SrcPort,
			RemoteAddr: ipHdr.Src,
			TCPStack:   tcpStack,
			SeqNum:     uint32(seqNum),
			Channel:    make(chan *protocol.IPPacket),
		}
		// Add to this TCP stack's connection table (each node needs a copy of this connection in it's OWN socket table)
		tcpStack.ConnectionsTable[fourTuple] = tcpConn
		tcpStack.NextSocketID++

		// Send a SYN-ACK back to client
		flags := header.TCPFlagSyn | header.TCPFlagAck
		err := tcpConn.sendTCP([]byte{}, uint32(flags), uint32(seqNum), uint32(tcpHdr.SeqNum+1))
		if err != nil {
			fmt.Println("Could not sent SYN packet")
			return err
		}
	} else if tcpHdr.Flags == (header.TCPFlagSyn | header.TCPFlagAck) {
		// Consult connections table for socket
		tcpConn, exists := tcpStack.ConnectionsTable[fourTuple]
		if !exists {
			return errors.New("associated connection not found on this node")
		}
		tcpConn.State = "ESTABLISHED"
		// Send ACK back to server
		flags := header.TCPFlagAck
		err := tcpConn.sendTCP([]byte{}, uint32(flags), tcpHdr.AckNum+1, tcpHdr.SeqNum+1)
		if err != nil {
			fmt.Println("Could not sent SYN packet")
			return err
		}
	}
	return nil
}

func (tcpStack *TCPStack) ListSockets() {
	fmt.Println("SID       LAddr     LPort      RAddr       RPort      Status")
	// Loop through all listener sockets on this node
	for _, socket := range tcpStack.ListenTable {
		fmt.Println(strconv.Itoa(int(socket.ID)) + " " + socket.LocalAddr.String() + " " + strconv.Itoa(int(socket.LocalPort)) + " " + socket.RemoteAddr.String() + " " + strconv.Itoa(int(socket.RemotePort)) + " " + socket.State)
	}
	// Loop through all connection sockets on this node
	for _, socket := range tcpStack.ConnectionsTable {
		fmt.Println(strconv.Itoa(int(socket.ID)) + " " + socket.LocalAddr.String() + " " + strconv.Itoa(int(socket.LocalPort)) + " " + socket.RemoteAddr.String() + " " + strconv.Itoa(int(socket.RemotePort)) + " " + socket.State)
	}
}

func (tcpStack *TCPStack) ACommand(port uint16) {
	listenConn, err := tcpStack.VListen(port)
	if err != nil {
		fmt.Println(err)
		return
	}
	for {
		conn, err := listenConn.VAccept()
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

func (tcpConn *TCPConn) VRead(buf []byte) (int, error) {

}

func (tcpStack *TCPStack) CCommand(ip netip.Addr, port uint16) {
	_, err := tcpStack.VConnect(ip, port)
	if err != nil {
		fmt.Println(err)
		return
	}
}
