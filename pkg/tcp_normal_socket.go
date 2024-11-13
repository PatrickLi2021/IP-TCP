package protocol

import (
	"bytes"
	"fmt"
	"io"
	tcp_utils "tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

const (
	maxPayloadSize = 10 // 1400 bytes - IP header size - TCP header size
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
		if (int32(tcpConn.RecvBuf.NXT)-tcpConn.RecvBuf.LBR) <= 1 && tcpConn.State != "CLOSED" {
			<-tcpConn.RecvSpaceOpen // Block until data is available
			continue
		}

		// Calculate how much data we can read
		bytesAvailable := uint32(tcp_utils.CalculateOccupiedRecvBufSpace(tcpConn.RecvBuf.LBR, tcpConn.RecvBuf.NXT))
		fmt.Println("In VRead, remaining space in rec buf = ")
		fmt.Println(bytesAvailable)
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

func (tcpConn *TCPConn) VWrite(data []byte) (int, error) {
	// Track the amount of data to write
	originalDataToSend := data
	bytesToWrite := len(data)
	for bytesToWrite > 0 {
		// Calculate remaining space in the send buffer
		remainingSpace := tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA)
		// Wait for space to become available if the buffer is full
		for remainingSpace <= 0 {
			<-tcpConn.SendSpaceOpen
			remainingSpace = tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA)
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
				payloadBuf[i] = tcpConn.SendBuf.Buffer[tcpConn.SendBuf.NXT % BUFFER_SIZE]
				tcpConn.SendBuf.NXT += 1
			}
			tcpConn.sendTCP(payloadBuf, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			tcpConn.SeqNum += uint32(bytesToSend)
			tcpConn.TotalBytesSent += uint32(payloadSize)
			bytesToSend = tcpConn.SendBuf.LBW - tcpConn.SendBuf.NXT + 1
		}
	}
}

func (tcpConn *TCPConn) WatchRecvBuf() error {
	for {
		nxt := tcpConn.RecvBuf.NXT
		lbr := tcpConn.RecvBuf.LBR
		// Indicates that there's space in receive buffer
		if int32(nxt)-lbr > 1 {
			tcpConn.RecvSpaceOpen <- true
		}
	}
}

// VWrite writes into your send buffer and that wakes up some thread that's watching your send buffer and then you send the packet
// On the other end, you have a thread that will wake up when you receive a segment. You load it into your receive buffer
func (tcpConn *TCPConn) NoDataAvailable(LBR uint32, NXT uint32) bool {
	if LBR == NXT {
		return true
	} else if LBR >= NXT {
		// If the only bytes left to read are null, then that equates to having no data
		return bytes.Equal(tcpConn.RecvBuf.Buffer[NXT:LBR], make([]byte, BUFFER_SIZE-NXT-LBR))
	} else {
		return bytes.Equal(tcpConn.RecvBuf.Buffer[LBR:NXT], make([]byte, BUFFER_SIZE-LBR-NXT))
	}
}
