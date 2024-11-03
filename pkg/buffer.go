package protocol

const BUFFER_SIZE = 1024

type TCPBuffer struct {
	Buffer []byte
	UNA uint32
	NXT uint32
	LBW uint32
	Rec_win uint32
	Channel chan *TCPConn // TODO: may change item sent through channel
}

