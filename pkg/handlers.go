package protocol

import (
	"fmt"
	"net/netip"
	"strconv"
	"time"
)

func TestPacketHandler(packet *IPPacket) {
	fmt.Println("Received test packet: Src: " + packet.Header.Src.String() +
		", Dst: " + packet.Header.Dst.String() +
		", TTL: " + strconv.Itoa(packet.Header.TTL) +
		", Data: " + string(packet.Payload))
}

func (stack *IPStack) RIPPacketHandler(packet *IPPacket) {
	ripPacket, err := UnmarshalRIP(packet.Payload)
	if err != nil {
		fmt.Println("error unmarshaling packet payload in rip packet handler")
		fmt.Println(err)
		return
	}

	if ripPacket.Command == 1 {
		// Have received a RIP request, need to send out an update
		destIP := packet.Header.Src

		ripUpdate := &RIPPacket{
			Command: 2,
		}

		entries := make([]RIPEntry, 0)
		stack.Mutex.RLock()
		for prefix, tuple := range stack.Forward_table {

			if tuple.Type == "S" {
				// forward table entry is a default route only, we don't send to other routers
				continue
			}

			// Convert IP address into uint32)
			addressInt, _ := ConvertToUint32(prefix.Addr())
			maskInt, _ := ConvertToUint32(prefix.Masked().Addr())

			entry := RIPEntry{
				Cost:    uint32(tuple.Cost),
				Address: addressInt,
				Mask:    maskInt,
			}
			if tuple.NextHopIP == destIP {
				// implement split horizon, cost = 16
				entry.Cost = 16
			}
			entries = append(entries, entry)
		}
		stack.Mutex.RUnlock()
		ripUpdate.Entries = entries
		ripUpdate.Num_entries = uint16(len(entries))

		ripBytes, err := MarshalRIP(ripUpdate)
		if err != nil {
			fmt.Println("error marshaling rip packet in rip packet handler")
			fmt.Println(err)
			return
		}
		stack.SendIP(nil, 32, destIP, 200, ripBytes)
	} else if ripPacket.Command == 2 {
		// list of entries that are new to send out for triggered update
		updatedEntries := make([]RIPEntry, 0)

		// received response, will need to update routing table
		entryUpdates := ripPacket.Entries
		stack.Mutex.Lock()
		for i := 0; i < int(ripPacket.Num_entries); i++ {
			entry := entryUpdates[i]
			entryAddress := netip.IPv4Unspecified()
			entryAddress, err := Uint32ToAddr(entry.Address, entryAddress)
			if err != nil {
				fmt.Println("error converting uint32 to net ip addr")
				fmt.Println(err)
				return
			}
			entryMask := netip.IPv4Unspecified()
			entryMask, err = Uint32ToAddr(entry.Mask, entryMask)
			if err != nil {
				fmt.Println("error converting uint32 to mask")
				fmt.Println(err)
				return
			}
			entryPrefix, err := entryAddress.Prefix(entryMask.BitLen() - 8)
			// TODO ^^^^^
			if err != nil {
				fmt.Println("error converting uint32 to net ip prefix")
				fmt.Println(err)
				return
			}

			prevTuple, exists := stack.Forward_table[entryPrefix]
			if !exists {
				// entry from neighbor does not exist, add to table
				stack.Forward_table[entryPrefix] = &ipCostInterfaceTuple{
					NextHopIP:   packet.Header.Src,
					Cost:        entry.Cost + 1,
					Interface:   nil,
					Type:        "R",
					LastRefresh: time.Now(),
				}

				entry.Cost = entry.Cost + 1
				updatedEntries = append(updatedEntries, entry)
			} else if exists && entry.Cost+1 < prevTuple.Cost {
				// entry exists and updated cost is lower than old, update table with new entry
				stack.Forward_table[entryPrefix].Cost = entry.Cost
				stack.Forward_table[entryPrefix].NextHopIP = packet.Header.Src
				stack.Forward_table[entryPrefix].LastRefresh = time.Now()
				entry.Cost = entry.Cost + 1
				updatedEntries = append(updatedEntries, entry)
			} else if exists && entry.Cost+1 > prevTuple.Cost {
				// updated cost greater than old cost
				if packet.Header.Src == prevTuple.NextHopIP {
					// topology has changed, route has higher cost now, update table
					stack.Forward_table[entryPrefix].Cost = entry.Cost
					stack.Forward_table[entryPrefix].LastRefresh = time.Now()
					entry.Cost = entry.Cost + 1
					updatedEntries = append(updatedEntries, entry)
				}
			} else if (exists && entry.Cost + 1 == prevTuple.Cost && packet.Header.Src == prevTuple.NextHopIP) {
				// repeat of same route, no update, but refresh time
				prevTuple.LastRefresh = time.Now()
			}
		}
		stack.Mutex.Unlock()

		// handles triggered updates
		if (len(updatedEntries) == 0) {
			return
		}
		for _, neighborIP := range stack.RipNeighbors {
			ripUpdate := &RIPPacket{
				Command:     2,
				Num_entries: uint16(len(updatedEntries)),
			}
			if (neighborIP != packet.Header.Src) {
				// no split horizon:
				ripUpdate.Entries = updatedEntries
			} else {
				// change costs to 16 for split horizon:
				entryCopies := make([]RIPEntry, 0)
				for _, entry := range updatedEntries {
					entryCopy := RIPEntry{
						Cost: 16,
						Address: entry.Address,
						Mask: entry.Mask,
					}
					entryCopies = append(entryCopies, entryCopy)
				}
				ripUpdate.Entries = entryCopies
			}
			
			ripBytes, err := MarshalRIP(ripUpdate)
			if err != nil {
				fmt.Println("error marshaling rip packet in rip packet handler")
				fmt.Println(err)
				return
			}
			stack.SendIP(nil, 32, neighborIP, 200, ripBytes)
		}
	}
}