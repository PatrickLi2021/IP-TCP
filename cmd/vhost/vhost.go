package main

import (
	"fmt"
	"ip-ip-pa/lnxconfig"
	"ip-ip-pa/pkg"
	"os"
)

const max_packet_size = 1400

func listen(stack *protocol.IPStack, iface *protocol.Interface) {
	// Since this is a host, we only listen on one interface
	for {
		stack.Receive(stack.Interfaces[iface.Name])
	}

}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: ./vhost --config <lnx file>")
		return
	}
	lnxFile := os.Args[1]

	// Parse the lnx file
	lnxConfig, err := lnxconfig.ParseConfig(lnxFile)
	if err != nil {
		panic(err)
	}
	// Create a new host node
	var stack *protocol.IPStack
	stack.Initialize(*lnxConfig)
	for _, iface := range stack.Interfaces {
		go listen(stack, iface)
	}

	for {
		// REPL
		var userInput string
		fmt.Scanln(&userInput)

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
			var spaceIdx = strings.Index(userInput[5:], " ")
			var ip = userInput[5:spaceIdx]
			var message = userInput[spaceIdx+1:]

			// Host will only have one interface
			var interface_name string
			var iface *protocol.Interface

			// Iterate through the map to get the key-value pair
			for key, val := range stack.Interfaces {
				interface_name = key
				iface = val
				break
			}
			stack.SendIP(iface.IP, 16, 0)
		} else {
			fmt.Println("Invalid command.")
			os.Exit(0)
		}
	}

}
