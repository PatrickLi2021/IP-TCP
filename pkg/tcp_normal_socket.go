package protocol

import (
	"errors"
	"fmt"
	"io"
	"time"

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
		if tcpConn.State == "CLOSE_WAIT" {
			if bytesRead > 0 {
				return bytesRead, nil
			}
			return 0, io.EOF
		}

		// Wait if there's no data available in the receive buffer
		if tcpConn.RecvBuf.CalculateOccupiedRecvBufSpace() == 0 {
			<-tcpConn.RecvBufferHasData // Block until data is available
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

func (tcpConn *TCPConn) VWrite(data []byte, finFlag bool) (int, error) {
	// Track the amount of data to write
	originalDataToSend := data
	bytesToWrite := len(data)
	for bytesToWrite > 0 {
		// Calculate remaining space in the send buffer
		remainingSpace := tcpConn.SendBuf.CalculateRemainingSendBufSpace()
		// Wait for space to become available if the buffer is full
		if remainingSpace <= 0 {
			<-tcpConn.SendSpaceOpen
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
		if len(tcpConn.SendBufferHasData) == 0 && toWrite != 0 {
			tcpConn.SendBufferHasData <- true
		}
		// Adjust the remaining data and update data slice
		bytesToWrite -= toWrite
		data = data[toWrite:]
	}

	// We have written all our data into the send buffer at this point (VClose calls this)
	if finFlag {
		tcpConn.SendBuf.FIN = tcpConn.SendBuf.LBW + 1
		if len(tcpConn.SendBufferHasData) == 0 {
			tcpConn.SendBufferHasData <- true
		}
	}
	return len(originalDataToSend), nil
}

// Monitors TCPConn's send buffer to send new data as it becomes available
func (tcpConn *TCPConn) SendSegment() {
	for {
		// Block until new data is available in the send buffer
		<-tcpConn.SendBufferHasData
		bytesToSend := tcpConn.SendBuf.LBW - tcpConn.SendBuf.NXT + 1
		// We continue sending, either for normal data or for ZWP
		bytesInFlight := uint32(tcpConn.SendBuf.NXT - tcpConn.SendBuf.UNA)

		for bytesToSend > 0 && tcpConn.ReceiverWin-bytesInFlight >= 0 && tcpConn.State != "FIN_WAIT_2" {

			// Zero-Window Probing
			if tcpConn.ReceiverWin == 0 {
				tcpConn.ZeroWindowProbe(tcpConn.SendBuf.NXT)
				tcpConn.SendBuf.NXT += 1
				tcpConn.SeqNum += 1
				bytesToSend -= 1
			}
			payloadSize := min(bytesToSend, maxPayloadSize, int32(tcpConn.ReceiverWin)-int32(bytesInFlight))
			if payloadSize > 0 {
				payloadBuf := make([]byte, payloadSize)
				for i := 0; i < int(payloadSize); i++ {
					payloadBuf[i] = tcpConn.SendBuf.Buffer[tcpConn.SendBuf.NXT%BUFFER_SIZE]
					tcpConn.SendBuf.NXT += 1
				}
				// Create packet and add to queue
				rtPacket := &RTPacket{
					Timestamp: time.Now(),
					SeqNum:    tcpConn.SeqNum,
					AckNum:    tcpConn.ACK,
					Data:      payloadBuf,
					Flags:     header.TCPFlagAck,
					NumTries:  0,
				}
				tcpConn.RetransmitStruct.RTQueue = append(tcpConn.RetransmitStruct.RTQueue, rtPacket)

				// Start RTO timer
				tcpConn.RetransmitStruct.RTOTimer = time.NewTicker(tcpConn.RetransmitStruct.RTO)

				tcpConn.sendTCP(payloadBuf, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
				tcpConn.SeqNum += uint32(payloadSize)
				tcpConn.TotalBytesSent += uint32(payloadSize)
				bytesToSend = tcpConn.SendBuf.LBW - tcpConn.SendBuf.NXT + 1
			}

			bytesInFlight = uint32(tcpConn.SendBuf.NXT - tcpConn.SendBuf.UNA)
		}
		if tcpConn.State == "FIN_WAIT_2" {
			fmt.Println("VWrite error: cannot send after transport endpoint shutdown")
		}

		// Check to see if FIN == LBW
		if tcpConn.SendBuf.NXT == tcpConn.SendBuf.FIN && tcpConn.SendBuf.FIN == tcpConn.SendBuf.LBW+1 {
			flags := header.TCPFlagFin | header.TCPFlagAck
			fmt.Println("Sent a FIN to initiate close")
			tcpConn.sendTCP([]byte{}, uint32(flags), tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
			if tcpConn.State == "ESTABLISHED" {
				tcpConn.State = "FIN_WAIT_1"
				tcpConn.SeqNum += 1
			}
		}
	}
}

func (tcpConn *TCPConn) CheckRTOTimer(rtStruct Retransmission) {
	for {
		select {
		// If ticker doesn't fire within RTO, retransmit
		case <-time.After(rtStruct.RTO):
			if len(rtStruct.RTQueue) > 0 {
				queueHead := rtStruct.RTQueue[0]
				// TODO: Potentially close socket
				if queueHead.NumTries == MAX_RETRIES {
					rtStruct.RTQueue = rtStruct.RTQueue[1:]
				}
				tcpConn.sendTCP(queueHead.Data, queueHead.Flags, queueHead.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
				// Increment numTries
				queueHead.NumTries++
				rtStruct.RTO = max(2*rtStruct.RTO, RTO_MAX)
			}
			// Restart retransmission timer
			rtStruct.RTOTimer.Stop()
			rtStruct.RTOTimer = time.NewTicker(rtStruct.RTO)
		}
	}
}

func (tcpConn *TCPConn) ZeroWindowProbe(nxt int32) {
	// Stop RTO timer for all zero window probes when entering zero window probing mode
	tcpConn.RetransmitStruct.RTOTimer.Stop()

	bytesInFlight := uint32(tcpConn.SendBuf.NXT - tcpConn.SendBuf.UNA)
	for tcpConn.ReceiverWin-bytesInFlight < maxPayloadSize {
		nextByte := tcpConn.SendBuf.Buffer[nxt%BUFFER_SIZE]
		probePayload := []byte{nextByte}
		tcpConn.sendTCP(probePayload, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK, tcpConn.CurWindow)
		// Wait some time before sending another probe
		time.Sleep(1 * time.Second) // TODO: change
		bytesInFlight = uint32(tcpConn.SendBuf.NXT - tcpConn.SendBuf.UNA)
	}
	// Restart timer
	tcpConn.RetransmitStruct.RTOTimer = time.NewTicker(tcpConn.RetransmitStruct.RTO)
}

func (tcpConn *TCPConn) VClose() error {
	// Check to see if conn is already in a closing state
	if tcpConn.State == "CLOSED" || tcpConn.State == "TIME_WAIT" || tcpConn.State == "LAST_ACK" || tcpConn.State == "CLOSING" {
		return nil
	}
	// TODO: Check to see if there is any unACK'ed data left? For now, there is no check
	if true {
		_, _ = tcpConn.VWrite([]byte{}, true)
		if tcpConn.State == "CLOSE_WAIT" {
			tcpConn.State = "LAST_ACK"
		}
		return nil
	} else {
		return errors.New("trying to close connection, but not all data has been sent and ACK'ed yet")
	}
}
