package protocol

const (
	maxPayloadSize = 1360 // 1400 bytes - IP header size - TCP header size
)

func (tcpConn *TCPConn) VRead(buf []byte) (int, error) {
	return 0, nil
}

func (tcpConn *TCPConn) VWrite(data []byte) (int, error) {
	bufferLen := len(tcpConn.SendBuf.Buffer)

	// Block until there's space available
	remainingSpace := bufferLen
	for remainingSpace <= 0 {
		<-tcpConn.SpaceOpen // Wait here if no space is available
		remainingSpace = len(tcpConn.SendBuf.Buffer) - int(tcpConn.SendBuf.LBW) + int(tcpConn.SendBuf.UNA)
	}

	// Determine how many bytes of data to write into send buffer
	bytesToWrite := len(data)
	if bytesToWrite > remainingSpace {
		bytesToWrite = remainingSpace
	}
	start := int(tcpConn.SendBuf.LBW % uint32(bufferLen))
	end := start + bytesToWrite

	if end <= bufferLen {
		// No wrap-around needed, just copy bytesToWrite into the send buffer
		copy(tcpConn.SendBuf.Buffer[start:end], data[:bytesToWrite])
	} else {
		// Wrap-around required
		section1 := bufferLen - start                                             // Bytes we can write up to the end of the buffer
		copy(tcpConn.SendBuf.Buffer[start:], data[:section1])                     // Write first part
		copy(tcpConn.SendBuf.Buffer[:end%bufferLen], data[section1:bytesToWrite]) // Write remaining part at the beginning
	}
	// Update LBW
	tcpConn.SendBuf.LBW = (tcpConn.SendBuf.LBW + uint32(bytesToWrite)) % uint32(bufferLen)

	// Send out the bytes

}

// VWrite writes into your send buffer and that wakes up some thread that's watching your send buffer and then you send the packet
// On the other end, you have a thread that will wake up when you receive a segment. You load it into your receive buffer
