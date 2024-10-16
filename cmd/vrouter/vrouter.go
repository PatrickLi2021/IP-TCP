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
				for mask, tuple := range stack.Forward_table {
					if tuple.Type == "S" {
						// default routes
						continue
					}
					if tuple.NextHopIP == neighborIP {
						// split horizon
						continue
					}

					// Convert IP address into uint32
					ipInteger, err := protocol.ConvertToUint32(tuple.NextHopIP)
					if err != nil {
						continue
					}

					// Convert prefix/mask into uint32
					prefixInteger, err := protocol.ConvertToUint32(mask.Addr())
					if err != nil {
						continue
					}
					entry := protocol.RIPEntry{
						Cost:    uint32(tuple.Cost),
						Address: ipInteger,
						Mask:    prefixInteger,
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

	// register test packet
	stack.RegisterRecvHandler(0, protocol.TestPacketHandler)

	// Router is now online, so we need to send RIP request to all of its neighbors
	if stack.RoutingType == 2 {
		stack.RegisterRecvHandler(200, stack.RIPPacketHandler)

		// send rip request to all rip neighbors
		for _, neighborIp := range stack.RipNeighbors {
			requestPacket := &protocol.RIPPacket{
				Command:     1,
				Num_entries: 0,
				Entries:     []protocol.RIPEntry{},
			}

			requestBytes, err := protocol.MarshalRIP(requestPacket)
			if err != nil {
				fmt.Println(err)
				return
			}
			stack.SendIP(nil, 32, neighborIp, 200, requestBytes)
		}
	}

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
			// TODO
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
	for {

		// Go through every route in the router's forwarding table
		for prefix, iFaceTuple := range stack.Forward_table {

			currentTime := time.Now()
			routeLastRefreshPlusTwelve := iFaceTuple.LastRefresh.Add(12 * time.Second)

			// Check if route is expired
			if currentTime.After(routeLastRefreshPlusTwelve) {
				stack.Mutex.Lock()
				stack.Forward_table[prefix].Cost = 16
				stack.Mutex.Unlock()
				// Get routing entries to send in updated
				entries := make([]protocol.RIPEntry, 0)
				stack.Mutex.RLock()
				for prefix, tuple := range stack.Forward_table {
					if tuple.Type == "S" {
						continue // this is a default route
					}

					// Convert IP address into uint32)
					addressInt, _ := protocol.ConvertToUint32(prefix.Addr())
					maskInt, _ := protocol.ConvertToUint32(prefix.Masked().Addr())

					entry := protocol.RIPEntry{
						Cost:    uint32(tuple.Cost),
						Address: addressInt,
						Mask:    maskInt,
					}
					entries = append(entries, entry)
				}
				stack.Mutex.RUnlock()
				// Send triggered update
				ripPacket := protocol.RIPPacket{
					Command:     2,
					Num_entries: uint16(len(entries)),
					Entries:     entries,
				}
				ripBytes, err := protocol.MarshalRIP(&ripPacket)
				if err != nil {
					fmt.Println("error marshaling rip packet in rip packet handler")
					fmt.Println(err)
					return
				}
				for _, neighborAddr := range stack.RipNeighbors {
					stack.SendIP(nil, 32, neighborAddr, 200, ripBytes)
				}

				// Delete entry from table
				stack.Mutex.Lock()
				delete(stack.Forward_table, prefix)
				stack.Mutex.Unlock()
			}
		}
	}
}

/*
Need for mutex - lock the forwarding table before you are editing it
*/
