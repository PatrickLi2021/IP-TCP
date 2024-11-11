package protocol

import (
	"fmt"
	"net/netip"
	"strconv"
)

func (tcpStack *TCPStack) ListSockets() {
	fmt.Println("SID  LAddr           LPort      RAddr          RPort    Status")
	// Loop through all sockets on this node
	for i := 0; i < int(tcpStack.NextSocketID); i++ {
		fourTuple := tcpStack.SocketIDToConn[uint32(i)]
		tcpConn, connExists := tcpStack.ConnectionsTable[*fourTuple]
		if connExists {
			tcpConn.ID = uint16(i)
			fmt.Println(strconv.Itoa(int(tcpConn.ID)) + "    " + fourTuple.srcAddr.String() + "        " + strconv.Itoa(int(fourTuple.srcPort)) + "      " + fourTuple.remoteAddr.String() + "       " + strconv.Itoa(int(fourTuple.remotePort)) + "     " + tcpConn.State)
		} else {
			listener, exists := tcpStack.ListenTable[fourTuple.srcPort]
			listener.ID = uint16(i)
			if !exists {
				fmt.Println("Error: socket could not be found in either table")
				return
			} else {
				fmt.Println(strconv.Itoa(int(listener.ID)) + "    " + fourTuple.srcAddr.String() + "        " + strconv.Itoa(int(fourTuple.srcPort)) + "      " + fourTuple.remoteAddr.String() + "       " + strconv.Itoa(int(fourTuple.remotePort)) + "     " + listener.State)
			}
		}
	}
}

func (tcpStack *TCPStack) ACommand(port uint16) {
	listenConn, err := tcpStack.VListen(port)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Created listen socket")
	for {
		_, err := listenConn.VAccept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("listen conn created")
	}
}

func (tcpStack *TCPStack) CCommand(ip netip.Addr, port uint16) {
	_, err := tcpStack.VConnect(ip, port)
	if err != nil {
		fmt.Println(err)
		return
	}
}

func (tcpStack *TCPStack) SCommand(socketID uint32, bytes string) {
	fourTuple, socketExists := tcpStack.SocketIDToConn[socketID]
	if !socketExists {
		fmt.Println("Error: Socket not found")
		return
	}
	tcpConn := tcpStack.ConnectionsTable[*fourTuple]
	bytesSent, _ := tcpConn.VWrite([]byte(bytes))
	fmt.Println("Sent " + strconv.Itoa(bytesSent) + " bytes")
}

func (tcpStack *TCPStack) RCommand(socketID uint32, numBytes uint32) {
	fourTuple := tcpStack.SocketIDToConn[socketID]
	tcpConn := tcpStack.ConnectionsTable[*fourTuple]
	appBuffer := make([]byte, BUFFER_SIZE)
	bytesRead, _ := tcpConn.VRead(appBuffer, numBytes)
	fmt.Println("Read " + strconv.Itoa(bytesRead) + " bytes: " + string(appBuffer[:numBytes]))
}
