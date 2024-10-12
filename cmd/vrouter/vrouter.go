package main

import (
	"bufio"
	"fmt"
	"ip-ip-pa/lnxconfig"
	protocol "ip-ip-pa/pkg"
	"ip-ip-pa/rip"
	"net/netip"
	"os"
	"strings"
)

var ripInstance *rip.RipInstance

func listen(stack *protocol.IPStack, iface *protocol.Interface) {
	for {
		stack.Receive(stack.Interfaces[iface.Name])
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

	// Router is now online, so we need to declare/initialize ripInstance specific struct
	// send RIP request to all of its neighbors
	if (stack.RoutingType == 2) {
		ripInstance.Initialize(lnxConfig)
		stack.RegisterRecvHandler(200, protocol.RIPPacketHandler)

		// send rip request to all rip neighbors
		for neighborIp := range ripInstance.NeighborRouters {
			requestPacket := &rip.RIPPacket{
				Command: 1,
				Num_entries: 0,
				Entries: []rip.RIPEntry{},
			}

			requestBytes, err := rip.MarshalRIP(requestPacket)
			if (err != nil) {
				fmt.Println(err)
				return
			}


			stack.SendIP(16, neighborIp, 200, requestBytes)
		}
	}

	// Start listening on all of its neighbors
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
			stack.Down()
			// TODO - iface name
		} else if userInput == "up" {
			// TODO - iface name
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

		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}
