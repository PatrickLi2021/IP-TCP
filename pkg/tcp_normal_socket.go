package protocol

import (
	"errors"
	"fmt"
	"io"

	"github.com/google/netstack/tcpip/header"
)

const (
	maxPayloadSize = 1360 // 1400 bytes - IP header size - TCP header size
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
			tcpConn.RecvBuf.Waiting = true
			fmt.Println("waiting for recv buffer chan has data")
			<-tcpConn.RecvBufferHasData // Block until data is available
			tcpConn.RecvBuf.Waiting = false
			// tcpConn.RecvBuf.freeSpace.Wait()
			fmt.Println("got the recv buffer has data chan")
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
		tcpConn.CurWindow += uint16(bytesRead)
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
		fmt.Println("Just in VWrite")
		// Calculate remaining space in the send buffer
		remainingSpace := tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		// fmt.Println("Here is the remaining space in the sendBuf: " + strconv.Itoa(int(remainingSpace)))
		// Wait for space to become available if the buffer is full
		for remainingSpace <= 0 {
			fmt.Println("blocking")
			<-tcpConn.SendSpaceOpen
			fmt.Println("done blocking")
			remainingSpace = tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		}

		// Determine how many bytes to actually write into the send buffer
		toWrite := min(bytesToWrite, remainingSpace)
		fmt.Println("Calculated ToWrite")
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
		fmt.Println("adjusted data")
		// fmt.Println(data)

	}
	return len(originalDataToSend), nil
}

// Monitors TCPConn's send buffer to send new data as it becomes available
func (tcpConn *TCPConn) SendSegment() {
	fmt.Println("in sendsegment function")
	for {
		fmt.Println("in sendsegment loop")
		// Block until new data is available in the send buffer
		<-tcpConn.SendBufferHasData
		fmt.Println("Read from send buffer has data")
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

func (tcpConn *TCPConn) VClose() error {
	// Check to see if conn is already in a closing state
	if tcpConn.State == "CLOSED" || tcpConn.State == "TIME_WAIT" || tcpConn.State == "LAST_ACK" || tcpConn.State == "CLOSING" {
		return nil
	}
	// TODO: Check to see if there is any unACK'ed data left. For now, there is no check
	if true {
		// If not, send FIN
		flags := header.TCPFlagFin | header.TCPFlagAck
		tcpConn.sendTCP([]byte{}, uint32(flags), tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
		if tcpConn.State == "ESTABLISHED" {
			tcpConn.State = "FIN_WAIT_1"
		}
		return nil
	} else {
		return errors.New("trying to close connection, but not all data has been sent and ACK'ed yet")
	}
}
