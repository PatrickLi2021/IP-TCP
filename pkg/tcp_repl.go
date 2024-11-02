package protocol

import (
	"fmt"
	"net/netip"
	"strconv"
)

func (tcpStack *TCPStack) ListSockets() {
	uniqueId := 0
	fmt.Println("SID  LAddr           LPort      RAddr          RPort    Status")
	// Loop through all listener sockets on this node
	for _, socket := range tcpStack.ListenTable {
		localAddrStr := formatAddr(socket.LocalAddr)
		remoteAddrStr := formatAddr(socket.RemoteAddr)
		fmt.Println(strconv.Itoa(uniqueId) + "    " + localAddrStr + "         " + strconv.Itoa(int(socket.LocalPort)) + "       " + remoteAddrStr + "        " + strconv.Itoa(int(socket.RemotePort)) + "        " + socket.State)
		uniqueId++
	}
	// Loop through all connection sockets on this node
	for _, socket := range tcpStack.ConnectionsTable {
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