package protocol

import (
	"fmt"
	"net/netip"
	"strconv"
)

func (tcpStack *TCPStack) ListSockets() {
	uniqueId := 0
	fmt.Println("SID  LAddr           LPort      RAddr          RPort    Status")
	// Loop through all sockets on this node
	for i := 0; i < int(tcpStack.NextSocketID); i++ {
		socket := tcpStack.SocketIDToConn[uint32(i)]
		fmt.Println(strconv.Itoa(uniqueId) + "    " + socket.LocalAddr.String() + "        " + strconv.Itoa(int(socket.LocalPort)) + "      " + socket.RemoteAddr.String() + "       " + strconv.Itoa(int(socket.RemotePort)) + "     " + socket.State)
		uniqueId++
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
		// else {
		// 	go conn.VRead()???
		// }
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
	tcpConn := tcpStack.SocketIDToConn[socketID]
	bytesSent, _ := tcpConn.VWrite([]byte(bytes))
	fmt.Println("Sent " + strconv.Itoa(bytesSent) + "bytes")
}

func (tcpStack *TCPStack) RCommand(socketID uint32, numBytes uint32) {
	tcpConn := tcpStack.SocketIDToConn[socketID]
	appBuffer := make([]byte, BUFFER_SIZE)
	bytesRead, _ := tcpConn.VRead(appBuffer, numBytes)
	fmt.Println("Read " + strconv.Itoa(bytesRead) + "bytes: " + string(appBuffer[:numBytes]))
}
