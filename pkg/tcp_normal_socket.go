package protocol

import (
	"fmt"
	"io"

	"github.com/google/netstack/tcpip/header"
)

const (
	maxPayloadSize = 3 // 1400 bytes - IP header size - TCP header size
)

func (tcpConn *TCPConn) VRead(buf []byte, maxBytes uint32) (int, error) {
	// If LBR == NXT, there is no data to read
	// We have the other 2 cases
	bytesRead := 0
	// Loop until we read some data
	for bytesRead == 0 {
		// If the connection is closed, return EOF if no data has been read
		if tcpConn.State == "CLOSED" {
			if bytesRead > 0 {
				return bytesRead, nil
			}
			return 0, io.EOF
		}
		// Wait if there's no data available in the receive buffer
		if tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace() == 0 {
			<-tcpConn.RecvBufferHasData // Block until data is available
			continue
		}

		// Calculate how much data we can read
		bytesAvailable := uint32(tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace())
		bytesToRead := min(bytesAvailable, maxBytes)

		lbr := tcpConn.RecvBuf.LBR

		for i := 0; i < int(bytesToRead); i++ {
			lbr += 1
			buf[i] = tcpConn.RecvBuf.Buffer[lbr%BUFFER_SIZE]
		}
		tcpConn.RecvBuf.LBR = lbr
		bytesRead += int(bytesToRead)
	}
	return bytesRead, nil
}

// func (tcpConn *TCPConn) ListenForACK() error {
// 	for {
// 		select {
// 		case ackNumber := <-tcpConn.AckReceived:
// 			// Retrieve ACK number from packet
// 			if int32(ackNumber) > tcpConn.SendBuf.UNA {
// 				// Move the UNA pointer and free up space in the send buffer
// 				tcpConn.SendBuf.UNA = int32(ackNumber)
// 				// Send signal through channel indicating that space has freed up
// 				tcpConn.SendSpaceOpen <- true
// 			}
// 			return nil
// 		default:
// 			// Channel was empty
// 			return errors.New("no ack number received from channel")
// 		}
// 	}
// }

func (tcpConn *TCPConn) WatchRecvBuf() {
	nxt := tcpConn.RecvBuf.NXT
	lbr := tcpConn.RecvBuf.LBR
	// Indicates that there's space in receive buffer
	if int32(nxt)-lbr > 1 {
		tcpConn.RecvBufferHasData <- true
	}
}

func (tcpConn *TCPConn) VWrite(data []byte) (int, error) {
	// Track the amount of data to write
	originalDataToSend := data
	bytesToWrite := len(data)
	for bytesToWrite > 0 {
		// Calculate remaining space in the send buffer
		remainingSpace := tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		// Wait for space to become available if the buffer is full
		for remainingSpace <= 0 {
			<-tcpConn.SendSpaceOpen
			fmt.Println("waiting for remaining space in VWrite")
			remainingSpace = tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		}

		// Determine how many bytes to actually write into the send buffer
		toWrite := min(bytesToWrite, remainingSpace)
		// Write data into the send buffer
		for i := 0; i < toWrite; i++ {
			tcpConn.SendBuf.Buffer[(int(tcpConn.SendBuf.LBW)+1+i)%BUFFER_SIZE] = data[i]
		}
		// Update the LBW pointer after writing data
		tcpConn.SendBuf.LBW = (tcpConn.SendBuf.LBW + int32(toWrite))
		// Send signal that there is now new data in send buffer
		tcpConn.SendBufferHasData <- true
		// Adjust the remaining data and update data slice
		bytesToWrite -= toWrite
		data = data[toWrite:]
	}
	return len(originalDataToSend), nil
}

// Monitors TCPConn's send buffer to send new data as it becomes available
func (tcpConn *TCPConn) SendSegment() {
	for {
		// Block until new data is available in the send buffer
		<-tcpConn.SendBufferHasData
		// Process the send buffer
		bytesToSend := tcpConn.SendBuf.LBW - tcpConn.SendBuf.NXT + 1
		for bytesToSend > 0 {
			payloadSize := min(bytesToSend, maxPayloadSize)
			payloadBuf := make([]byte, payloadSize)
			for i := 0; i < int(payloadSize); i++ {
				payloadBuf[i] = tcpConn.SendBuf.Buffer[tcpConn.SendBuf.NXT%BUFFER_SIZE]
				tcpConn.SendBuf.NXT += 1
			}
			tcpConn.sendTCP(payloadBuf, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			tcpConn.SeqNum += uint32(payloadSize)
			tcpConn.TotalBytesSent += uint32(payloadSize)
			bytesToSend = tcpConn.SendBuf.LBW - tcpConn.SendBuf.NXT + 1
		}
	}
}

// VWrite writes into your send buffer and that wakes up some thread that's watching your send buffer and then you send the packet
// On the other end, you have a thread that will wake up when you receive a segment. You load it into your receive buffer
// func (tcpConn *TCPConn) NoDataAvailable(LBR uint32, NXT uint32) bool {
// 	if LBR == NXT {
// 		return true
// 	} else if LBR >= NXT {
// 		// If the only bytes left to read are null, then that equates to having no data
// 		return bytes.Equal(tcpConn.RecvBuf.Buffer[NXT:LBR], make([]byte, BUFFER_SIZE-NXT-LBR))
// 	} else {
// 		return bytes.Equal(tcpConn.RecvBuf.Buffer[LBR:NXT], make([]byte, BUFFER_SIZE-LBR-NXT))
// 	}
// }
