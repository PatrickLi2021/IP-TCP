package protocol

import (
	tcp_utils "tcp-tcp-team-pa/iptcp_utils"

	"github.com/google/netstack/tcpip/header"
)

const (
	maxPayloadSize = 1360 // 1400 bytes - IP header size - TCP header size
)

func (tcpConn *TCPConn) VRead(buf []byte) (int, error) {
	return 0, nil
}

func (tcpConn *TCPConn) VWrite(data []byte) (int, error) {
	// Block until there's space available
	// TODO: 
	bytesToWrite := len(data)
	remainingSpace := tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA);
	for bytesToWrite > 0 {
		if (remainingSpace <= 0) {
			<-tcpConn.SpaceOpen // Wait here if no space is available
			// recalc remaining space once we get trigger that space has opened up
			remainingSpace = tcp_utils.CalculateRemainingSendBufSpace(tcpConn.SendBuf.LBW, tcpConn.SendBuf.UNA);
		}

		// Determine how many bytes of data to write into send buffer
		if bytesToWrite > remainingSpace {
			bytesToWrite = remainingSpace
		}

		for i := 1; i <= bytesToWrite; i ++ {
			// writes data into send buf
			tcpConn.SendBuf.Buffer[ (int(tcpConn.SendBuf.LBW) + i) % BUFFER_SIZE] = data[i-1]
		}
		// moves LBW pointer
		tcpConn.SendBuf.LBW = (tcpConn.SendBuf.LBW + uint32(bytesToWrite)) & BUFFER_SIZE

		bytesToWrite = len(data) - bytesToWrite
		// update data byte arr to remove bytes already written
		data = data[bytesToWrite:]
	}

	// TODO: trigger sending bytes
	<- tcpConn.SendBuf.Channel
}

func (tcpConn *TCPConn) SendSegment() (error) {
	value := <-tcpConn.SpaceOpen
	if (!value) {
		// no space, send segments from send buf
		nxt := tcpConn.SendBuf.NXT
		lbw := tcpConn.SendBuf.LBW
		end_idx := lbw + 1
		if (nxt <= lbw) {
			bytesToSend := lbw - nxt + 1
			if (bytesToSend > maxPayloadSize) {
				end_idx = nxt + maxPayloadSize
			}
			tcpConn.sendTCP(tcpConn.SendBuf.Buffer[nxt: end_idx], header.TCPFlagAck, )
		}
		// TODO: track ISN or curr Seq #???

	}
}





// VWrite writes into your send buffer and that wakes up some thread that's watching your send buffer and then you send the packet
// On the other end, you have a thread that will wake up when you receive a segment. You load it into your receive buffer
