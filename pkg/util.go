package protocol

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

func ConvertToUint32(input netip.Addr) (uint32, error) {
	if input.Is4() {
		bytes, err := input.MarshalBinary()
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint32(bytes), nil
	}
	return 0, fmt.Errorf("input is not IPv4")
}

func Uint32ToAddr(input uint32, addr netip.Addr) (netip.Addr, error) {
	buf := make([]byte, 4) // 4 bytes for uint32.
	binary.BigEndian.PutUint32(buf, input)
	err := addr.UnmarshalBinary(buf)
	if err != nil {
		return addr, err
	}
	return addr, nil
}