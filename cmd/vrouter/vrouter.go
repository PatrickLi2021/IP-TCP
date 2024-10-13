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

func routerPeriodicSend(stack *protocol.IPStack, ripInstance *protocol.RipInstance) {
	// function to send router updates to rip neighbors every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:

			ripUpdate := &protocol.RIPPacket{
				Command: 2,
			}

			entries := make([]protocol.RIPEntry, 0)
			for mask, tuple := range stack.Forward_table {
				if tuple.Cost == 0 && tuple.Interface == nil {
					// default routes
					continue
				}

				// Convert IP address into uint32
				ipInteger, _, _ := protocol.ConvertToUint32(tuple.NextHopIP)

				// Convert prefix/mask into uint32
				prefixInteger, _, _ := protocol.ConvertToUint32(mask)
				entry := protocol.RIPEntry{
					Cost:    uint32(tuple.Cost),
					Address: ipInteger,
					Mask:    prefixInteger,
				}
				entries = append(entries, entry)
			}

			ripUpdate.Entries = entries
			ripUpdate.Num_entries = uint16(len(entries))

			ripBytes, err := protocol.MarshalRIP(ripUpdate)
			if err != nil {
				fmt.Println("error marshaling rip packet in rip packet handler")
				fmt.Println(err)
				return
			}

			for _, neighborIP := range ripInstance.NeighborRouters {
				stack.SendIP(nil, 16, neighborIP, 200, ripBytes)
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
	var ripInstance *protocol.RipInstance = &protocol.RipInstance{}
	stack.Initialize(*lnxConfig)
	fmt.Println("Here is the router forward table")
	fmt.Println(stack.Forward_table)
	fmt.Println()

	// register test packet
	stack.RegisterRecvHandler(0, protocol.TestPacketHandler)
	fmt.Println("in vrouter")
	fmt.Println(stack.RoutingType)

	// Router is now online, so we need to declare/initialize ripInstance specific struct
	// send RIP request to all of its neighbors
	if stack.RoutingType == 2 {
		ripInstance.Initialize(*lnxConfig)
		stack.RegisterRecvHandler(200, stack.RIPPacketHandler)

		// send rip request to all rip neighbors
		fmt.Println("SENDING RIP REQUEST UPON INITIALIZE")
		fmt.Println("------------------------------------")
		for _, neighborIp := range ripInstance.NeighborRouters {
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
			fmt.Println(neighborIp)
			stack.SendIP(nil, 16, neighborIp, 200, requestBytes)
		}
	}

	// thread to send router udpates to rip neighbors every 5 secs
	// go routerPeriodicSend(stack, ripInstance)

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
		} else if userInput == "down" {
			interfaceName := userInput[5:]
			stack.Down(interfaceName)

		} else if userInput == "up" {
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

			stack.SendIP(nil, 16, destIP, 0, []byte(message))

		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}
