package protocol

const BUFFER_SIZE = 10

type TCPSendBuf struct {
	Buffer  []byte
	UNA     int32 // Represents oldest un-ACKed segment (updated by TCP stack)
	NXT     int32 // Represents data in the buffer that has been sent (updated by TCP stack)
	LBW     int32 // Represents data written into the buffer via VWrite() (updated by app)
	Rec_win int32
	Channel chan bool
}

type TCPRecvBuf struct {
	Buffer []byte
	LBR    int32  // Represents the last byte read (updated by app)
	NXT    uint32 // Represents how much data we've received (next byte we expect to receive)
	// NXT is updated by your TCP stack (internal packet events)
}

// Data between NXT and LBW is data that's in the buffer but not yet sent
// Data between UNA and NXT is data that's sent but not yet ACKed

// On the receiving side, as you get packet events, you advance the NXT pointer
// Data between LBR and NXT represents data that has not been read/removed, but has been received in order
