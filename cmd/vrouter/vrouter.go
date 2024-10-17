package main

import (
	"bufio"
	"fmt"
	"ip-ip-pa/lnxconfig"
	protocol "ip-ip-pa/pkg"
	"net/netip"
	"os"
	"strings"
	"time"
)

func listen(stack *protocol.IPStack, iface *protocol.Interface) {
	for {
		// All logic for updating other router's tables is handled in receive()
		stack.Receive(stack.Interfaces[iface.IP])
	}
}

func routerPeriodicSend(stack *protocol.IPStack) {
	// function to send router updates to rip neighbors every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// split horizon
			for _, neighborIP := range stack.RipNeighbors {
				entries := make([]protocol.RIPEntry, 0)
				// loop to accumulate entries to send as periodic update
				stack.Mutex.RLock()
				for prefix, tuple := range stack.Forward_table {
					if tuple.Type == "S" {
						// default routes
						continue
					}

					// Convert IP address into uint32
					ipInteger, err := protocol.ConvertAddrToUint32(tuple.NextHopIP)
					if err != nil {
						continue
					}

					// Convert prefix into mask into uint32
					prefixInteger, err := protocol.ConvertPrefixToUint32(prefix)
					if err != nil {
						continue
					}
					entry := protocol.RIPEntry{
						Cost:    uint32(tuple.Cost),
						Address: ipInteger,
						Mask:    prefixInteger,
					}
					if tuple.NextHopIP == neighborIP {
						// split horizon
						entry.Cost = 16
					}
					entries = append(entries, entry)
				}
				stack.Mutex.RUnlock()
				if len(entries) > 0 {
					ripUpdate := &protocol.RIPPacket{
						Command: 2,
					}
					ripUpdate.Entries = entries
					ripUpdate.Num_entries = uint16(len(entries))

					ripBytes, err := protocol.MarshalRIP(ripUpdate)
					if err != nil {
						fmt.Println("error marshaling rip packet in rip packet handler")
						fmt.Println(err)
						return
					}
					stack.SendIP(nil, 32, neighborIP, 200, ripBytes)
				}
			}

		}
	}
}

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: ./vrouter --config <lnx file>")
	}
	lnxFile := os.Args[2]

	// Parse the lnx file
	lnxConfig, err := lnxconfig.ParseConfig(lnxFile)
	if err != nil {
		fmt.Println("Error parsing config file")
		return
	}
	// Create a new router node
	var stack *protocol.IPStack = &protocol.IPStack{}
	stack.Initialize(*lnxConfig)

	// thread to send router udpates to rip neighbors every 5 secs
	go routerPeriodicSend(stack)
	// thread to check for expired routes
	go cleanExpiredRoutes(stack)

	// Start listening on all of its interfaces
	for _, iface := range stack.Interfaces {
		go listen(stack, iface)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Enter command")
	for scanner.Scan() {
		userInput := scanner.Text()

		if userInput == "li" {
			fmt.Println(stack.Li())
		} else if userInput == "ln" {
			fmt.Println(stack.Ln())
		} else if userInput == "lr" {
			fmt.Println(stack.Lr())
		} else if len(userInput) >= 6 && userInput[0:4] == "down" {
			interfaceName := userInput[5:]
			stack.Down(interfaceName)

		} else if len(userInput) >= 4 && userInput[0:2] == "up" {
			interfaceName := userInput[3:]
			stack.Up(interfaceName)

		} else if userInput[0:4] == "send" {
			var spaceIdx = strings.Index(userInput[5:], " ") + 5

			destIP, err := netip.ParseAddr(userInput[5:spaceIdx])
			if err != nil {
				fmt.Println("Please enter a valid IP address after send")
				continue
			}
			var message = userInput[spaceIdx+1:]
			if len(message) <= 0 {
				fmt.Println("Please enter a valid message to send after the IP address")
				continue
			}

			stack.SendIP(nil, 32, destIP, 0, []byte(message))

		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}

func cleanExpiredRoutes(stack *protocol.IPStack) {

	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			deletedEntries := make([]protocol.RIPEntry, 0)
			// Go through every route in the router's forwarding table
			stack.Mutex.Lock()
			for prefix, iFaceTuple := range stack.Forward_table {
				if (iFaceTuple.Type == "L" || iFaceTuple.Type == "S") {
					// skip over local routes and static, default routes
					continue
				}

				if ( (time.Since(iFaceTuple.LastRefresh)) * time.Second > (12 * time.Second) ) {
					// route is expired
					stack.Forward_table[prefix].Cost = 16

					// make new entry to to add list of deleted entries to send in triggered update
					addressInt, err := protocol.ConvertAddrToUint32(prefix.Addr())
					if (err != nil) {
						continue
					}
					maskInt, err := protocol.ConvertPrefixToUint32(prefix)
					if (err != nil) {
						continue
					}
					ripEntry := protocol.RIPEntry{
						Cost: 16,
						Address: addressInt,
						Mask: maskInt,
					}
					deletedEntries = append(deletedEntries, ripEntry)
				}
			}
			stack.Mutex.Unlock()

			// send triggered update, don't account for split horizon, because all changed routes have cost 16
			if (len(deletedEntries) > 0) {
				continue
			}
			for _, neighborIP := range stack.RipNeighbors {
				ripUpdate := &protocol.RIPPacket{
					Command: 2,
					Num_entries: uint16(len(deletedEntries)),
					Entries: deletedEntries,
				}
				ripBytes, err := protocol.MarshalRIP(ripUpdate)
				if (err != nil) {
					continue
				}
				stack.SendIP(nil, 32, neighborIP, 200, ripBytes)
				
			}

			// delete expired routes from forwarding table
			stack.Mutex.Lock()
			for _, entry := range deletedEntries {
				entryAddress, err := protocol.Uint32ToAddr(entry.Address)
				if err != nil {
					fmt.Println("error converting uint32 to net ip addr")
					fmt.Println(err)
					continue
				}
				entryPrefix, err := protocol.ConvertUint32ToPrefix(entry.Mask, entryAddress)
				if err != nil {
					fmt.Println("error converting uint32 to prefix")
					fmt.Println(err)
					continue
				}
				delete(stack.Forward_table, entryPrefix)
			}
			stack.Mutex.Unlock()
		}

	}

}

// TODO maybe have time channel for ever 12 seconds?
