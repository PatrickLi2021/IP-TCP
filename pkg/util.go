package protocol

import (
	"encoding/binary"
	"math/bits"
	"net/netip"
)

func ConvertAddrToUint32(input netip.Addr) (uint32, error) {
	bytes := input.As4()
	return binary.BigEndian.Uint32(bytes[:]), nil
}

func Uint32ToAddr(input uint32) (netip.Addr, error) {
	buf := make([]byte, 4) // 4 bytes for uint32.
	binary.BigEndian.PutUint32(buf, input)
	var byteArray [4]byte = [4]byte{buf[0], buf[1], buf[2], buf[3]}
	return netip.AddrFrom4(byteArray), nil
}

func ConvertPrefixToUint32(input netip.Prefix) (uint32, error) {
	bytes := make([]byte, 4)
	maskLen := input.Masked().Bits() / 8

	for i := 0; i < maskLen; i++ {
		bytes[i] = 255
	}
	for j := maskLen; j < 4; j++ {
		bytes[j] = 0
	}
	return binary.BigEndian.Uint32(bytes), nil
}

func ConvertUint32ToPrefix(input uint32, addr netip.Addr) (netip.Prefix, error) {
	maskLen := bits.OnesCount32(input)
	prefix, err := addr.Prefix(maskLen)
	return prefix, err
}

func formatAddr(addr netip.Addr) string {
	// Check if addr is equal to the zero value of netip.Addr
	if !addr.IsValid() {
		return "*"
	}
	return addr.String()
}