package main

import (
	"bufio"
	"fmt"
	"ip-ip-pa/lnxconfig"
	protocol "ip-ip-pa/pkg"
	"net/netip"
	"os"
	"strings"
)

func listen(stack *protocol.IPStack, iface *protocol.Interface) {
	// Since this is a host, we only listen on one interface
	for {
		stack.Receive(stack.Interfaces[iface.IP])
	}

}

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: ./vhost --config <lnx file>")
		return
	}
	lnxFile := os.Args[2]

	// Parse the lnx file
	lnxConfig, err := lnxconfig.ParseConfig(lnxFile)
	if err != nil {
		fmt.Println("error parsing config file")
		return
	}
	// Create a new host node
	var stack *protocol.IPStack = &protocol.IPStack{}
	stack.Initialize(*lnxConfig)

	stack.RegisterRecvHandler(0, protocol.TestPacketHandler)
	for _, iface := range stack.Interfaces {
		go listen(stack, iface)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Enter command:")
	for scanner.Scan() {
		// REPL
		userInput := scanner.Text()

		if userInput == "li" {
			fmt.Println(stack.Li())

		} else if userInput == "ln" {
			fmt.Println(stack.Ln())

		} else if userInput == "lr" {
			fmt.Println(stack.Lr())

		} else if userInput[0:4] == "down" {
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

			// Host will only have one interface
			var iface *protocol.Interface

			// Iterate through the map to get the key-value pair
			for _, val := range stack.Interfaces {
				iface = val
				break
			}
			stack.SendIP(&iface.IP, 16, destIP, 0, []byte(message))
		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}
