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
	fmt.Println("FILE SIZE" + strconv.Itoa(fileSize))

	// Block until the connection is fully established before we call sendSegment()
	<-tcpConn.SfRfEstablished
	go tcpConn.SendSegment()

	for bytesSent < fileSize {
		// Read into data how much available space there is in send buffer
		buf_space := tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		fmt.Println("BUF SPACE: " + strconv.Itoa(buf_space))
		data_len := min(buf_space, fileSize)
		fmt.Println()
		fmt.Println("Data Len: " + strconv.Itoa(data_len))
		data := make([]byte, data_len)
		_, err = file.Read(data)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return err
		}
		// Call VWrite()
		bytesWritten, _ := tcpConn.VWrite(data)
		bytesSent += bytesWritten
	}
	fmt.Println("Sent " + strconv.Itoa(bytesSent) + " bytes")
	// TODO: ADD A CALL TO VCLOSE HERE ONCE IT IS IMPLEMENTED
	return nil
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
	// TODO: Continue reading as long as the connection stays open

	bytesReceived := 0
	for bytesReceived < 11 {
		// Calculate how much data can be read in
		availableSpace := BUFFER_SIZE - tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace()
		buf := make([]byte, availableSpace)
		n, err := tcpConn.VRead(buf, uint32(availableSpace))
		if err != nil {
			fmt.Println(err)
			return err
		}
		if n != 0 {
			fmt.Println("Ronaldo")
			bytesWritten, write_err := outFile.Write(buf[:n])
			fmt.Println("Messi")
			bytesReceived += bytesWritten
			if write_err != nil {
				fmt.Println(err)
				return err
			}
		}
	}
	return nil
}
