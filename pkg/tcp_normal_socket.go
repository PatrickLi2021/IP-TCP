package protocol

import (
	"errors"
	"io"
	tcp_utils "tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

const (
	maxPayloadSize = 1360 // 1400 bytes - IP header size - TCP header size
)

func (tcpConn *TCPConn) VRead(buf []byte, maxBytes uint32) (int, error) {
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
		if tcpConn.RecvBuf.NXT == tcpConn.RecvBuf.LBR && tcpConn.State != "CLOSED" {
			<-tcpConn.RecvSpaceOpen // Block until data is available
			continue
		}

		// Calculate how much data we can read
		bytesAvailable := uint32(tcp_utils.CalculateRemainingRecvBufSpace(tcpConn.RecvBuf.LBR, tcpConn.RecvBuf.NXT))
		bytesToRead := min(bytesAvailable, maxBytes)

		lbr := tcpConn.RecvBuf.LBR
		nxt := tcpConn.RecvBuf.NXT

		// Read data based on buffer wrap-around conditions
		if lbr <= nxt {
			// Direct read when LBR is before or at NXT
			n := copy(buf[bytesRead:], tcpConn.RecvBuf.Buffer[lbr:lbr+bytesToRead])
			bytesRead += n
			tcpConn.RecvBuf.LBR = (lbr + uint32(n)) % BUFFER_SIZE
		} else {
			// Wrapped buffer, read in two parts
			firstChunk := copy(buf[bytesRead:], tcpConn.RecvBuf.Buffer[lbr:])
			bytesRead += firstChunk

			if uint32(firstChunk) < bytesToRead {
				secondChunk := copy(buf[bytesRead:], tcpConn.RecvBuf.Buffer[:bytesToRead-uint32(firstChunk)])
				bytesRead += secondChunk
				tcpConn.RecvBuf.LBR = uint32(secondChunk)
			} else {
				tcpConn.RecvBuf.LBR = (lbr + uint32(firstChunk)) % BUFFER_SIZE
			}
		}
	}

	return bytesRead, nil
}

func (tcpConn *TCPConn) ListenForACK() error {
	for {
		select {
		case ackNumber := <-tcpConn.AckReceived:
			// Retrieve ACK number from packet
			if ackNumber > tcpConn.SendBuf.UNA {
				// Move the UNA pointer and free up space in the send buffer
				tcpConn.SendBuf.UNA = ackNumber
				// Send signal through channel indicating that space has freed up
				tcpConn.SendSpaceOpen <- true
			}
			return nil
		default:
			// Channel was empty
			return errors.New("no ack number received from channel")
		}
	}
}

func (tcpConn *TCPConn) VWrite(data []byte) (int, error) {
	// Block until there's space available
	bytesToWrite := len(data)
	remainingSpace := tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA)
	for bytesToWrite > 0 {
		if remainingSpace <= 0 {
			// Buffer is full, so send a segment and listen for ACKs
			tcpConn.SendSegment()
			go tcpConn.ListenForACK()
			<-tcpConn.SendSpaceOpen // Wait here if no space is available
			// recalc remaining space once we get trigger that space has opened up
			remainingSpace = tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA)
		}

		// Determine how many bytes of data to write into send buffer
		if bytesToWrite > remainingSpace {
			bytesToWrite = remainingSpace
		}

		for i := 1; i <= bytesToWrite; i++ {
			// writes data into send buf
			tcpConn.SendBuf.Buffer[(int(tcpConn.SendBuf.LBW)+i)%BUFFER_SIZE] = data[i-1]
		}
		// moves LBW pointer
		tcpConn.SendBuf.LBW = (tcpConn.SendBuf.LBW + uint32(bytesToWrite)) % BUFFER_SIZE

		bytesToWrite = len(data) - bytesToWrite
		// update data byte arr to remove bytes already written
		data = data[bytesToWrite:]
	}
	return len(data), nil
}

func (tcpConn *TCPConn) SendSegment() error {
	select {
	case value := <-tcpConn.SendSpaceOpen:
		// Channel had data, and value was received
		if !value {
			// no space, send segments from send buf
			nxt := tcpConn.SendBuf.NXT
			lbw := tcpConn.SendBuf.LBW
			end_idx := lbw + 1
			if nxt <= lbw {
				bytesToSend := lbw - nxt + 1
				if bytesToSend > maxPayloadSize {
					end_idx = nxt + maxPayloadSize
				}
				tcpConn.sendTCP(tcpConn.SendBuf.Buffer[nxt:end_idx], header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK)
			} else {
				// If NXT > LBW, we split up our segment into 2 bytes (since LBW has wrapped around to the beginning
				// of the buffer, hence why it is now less than NXT)

				// Case where distance from NXT to end of buffer exceeds max payload size
				if uint32(len(tcpConn.SendBuf.Buffer))-nxt > maxPayloadSize {
					end_idx = nxt + maxPayloadSize
					tcpConn.sendTCP(tcpConn.SendBuf.Buffer[nxt:end_idx], header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK)
					// Increment NXT pointer
					tcpConn.SendBuf.NXT = (tcpConn.SendBuf.NXT + uint32(maxPayloadSize)) % BUFFER_SIZE
					return nil
				}
				// Case where we have to break it up into 2 chunks
				remainingSpace := maxPayloadSize - (uint32(len(tcpConn.SendBuf.Buffer)) - nxt)
				firstChunk := tcpConn.SendBuf.Buffer[nxt:]
				secondChunk := tcpConn.SendBuf.Buffer[:remainingSpace]
				bytesToSend := append(firstChunk, secondChunk...)
				tcpConn.sendTCP(bytesToSend, header.TCPFlagAck, tcpConn.SeqNum, tcpConn.ACK)
				// Increment NXT pointer
				tcpConn.SendBuf.NXT = remainingSpace
			}
		}
		return errors.New("send buffer is full")
	default:
		// Channel was empty
		return errors.New("no value received from channel")
	}
}

// VWrite writes into your send buffer and that wakes up some thread that's watching your send buffer and then you send the packet
// On the other end, you have a thread that will wake up when you receive a segment. You load it into your receive buffer
