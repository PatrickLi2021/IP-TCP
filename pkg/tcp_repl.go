package protocol

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
)

func (tcpStack *TCPStack) ListSockets() {
	fmt.Println("SID  LAddr           LPort      RAddr          RPort    Status")
	// Loop through all sockets on this node
	for _, fourTuple := range tcpStack.SocketIDToConn {
		tcpConn, connExists := tcpStack.ConnectionsTable[*fourTuple]
		if connExists {
			fmt.Println(strconv.Itoa(int(tcpConn.ID)) + "    " + fourTuple.srcAddr.String() + "        " + strconv.Itoa(int(fourTuple.srcPort)) + "      " + fourTuple.remoteAddr.String() + "       " + strconv.Itoa(int(fourTuple.remotePort)) + "     " + tcpConn.State)
		} else {
			listener, exists := tcpStack.ListenTable[fourTuple.srcPort]
			if !exists {
				fmt.Println()
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
		tcpConn, err := listenConn.VAccept()
		if err != nil {
			fmt.Println(err)
			return
		}
		go tcpConn.CheckRTOTimer()
		go tcpConn.SendSegment()
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
	bytesSent, _ := tcpConn.VWrite([]byte(bytes), false)
	fmt.Println("Sent " + strconv.Itoa(bytesSent) + " bytes")
}

func (tcpStack *TCPStack) RCommand(socketID uint32, numBytes uint32) {
	fourTuple := tcpStack.SocketIDToConn[socketID]
	tcpConn := tcpStack.ConnectionsTable[*fourTuple]
	appBuffer := make([]byte, BUFFER_SIZE)
	bytesRead, _ := tcpConn.VRead(appBuffer, numBytes)
	fmt.Println("Read " + strconv.Itoa(bytesRead) + " bytes: " + string(appBuffer))
}

func (tcpStack *TCPStack) SfCommand(filepath string, addr netip.Addr, port uint16) error {
	tcpConn, err := tcpStack.VConnect(addr, port)
	if err != nil {
		fmt.Println(err)
		return err
	}
	// Open file
	file, err := os.Open(filepath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return err
	}
	defer file.Close()

	// Read from file
	bytesSent := 0
	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println(err)
		return err
	}
	fileSize := int(fileInfo.Size())

	// Block until the connection is fully established before we call sendSegment()
	<-tcpConn.SfRfEstablished
	go tcpConn.CheckRTOTimer()
	go tcpConn.SendSegment()

	for bytesSent < fileSize {
		// Read into data how much available space there is in send buffer
		buf_space := tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		if buf_space <= 0 {
			<-tcpConn.SendSpaceOpen
		}
		buf_space = tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		data_len := min(buf_space, fileSize-bytesSent)
		data := make([]byte, data_len)
		_, err = file.Read(data)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return err
		}
		// Call VWrite()
		bytesWritten, _ := tcpConn.VWrite(data, false)
		bytesSent += bytesWritten
	}
	err = tcpConn.VClose()
	fmt.Println("Sent " + strconv.Itoa(bytesSent) + " bytes")
	return err
}

func (tcpStack *TCPStack) RfCommand(filepath string, port uint16) error {
	// Call VListen
	tcpListener, _ := tcpStack.VListen(port)

	// Call VAccept
	tcpConn, _ := tcpListener.VAccept()
	// Open file to read into
	outFile, err := os.Create(filepath)
	if err != nil {
		fmt.Println(err)
	}
	defer outFile.Close()

	go tcpConn.SendSegment()

	bytesReceived := 0

	for (tcpConn.OtherSideLastSeq == 0) || (tcpConn.OtherSideLastSeq != 0) && tcpConn.RecvBuf.LBR < (int32(tcpConn.OtherSideLastSeq)-int32(tcpConn.OtherSideISN)-2) { // Calculate how much data can be read in
		toRead := tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()
		for toRead <= 0 {
			<-tcpConn.RecvBufferHasData // Block until data is available
			// tcpConn.RecvBuf.freeSpace.Wait()
			toRead = tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()
		}
		toRead = tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()
		buf := make([]byte, toRead)
		n, err := tcpConn.VRead(buf, uint32(toRead))
		bytesReceived += n
		if err != nil {
			fmt.Println(err)
			return err
		}
		if n != 0 {
			_, write_err := outFile.Write(buf[:n])
			if write_err != nil {
				fmt.Println(err)
				return err
			}
		}
	}
	err = tcpConn.VClose()

	// delete listen socket
	delete(tcpStack.ListenTable, tcpListener.LocalPort)
	delete(tcpStack.SocketIDToConn, tcpConn.ID)
	delete(tcpStack.SocketIDToConn, tcpListener.ID)
	fmt.Println("Received " + strconv.Itoa(bytesReceived) + " bytes")
	return err
}

func (tcpStack *TCPStack) CloseCommand(socketId uint32) error {
	tuple, ok := tcpStack.SocketIDToConn[socketId]
	if ok {
		if tuple.remotePort == 0 {
			// listener
			delete(tcpStack.ListenTable, tuple.srcPort)
			delete(tcpStack.SocketIDToConn, socketId)
			return nil
		} else {
			// normal socket
			tcpConn := tcpStack.ConnectionsTable[*tuple]
			return tcpConn.VClose()
		}
	}
	return nil
}
