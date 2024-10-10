package main

import (
	"fmt"
	"ip-ip-pa/pkg/protocol"
	"log"
	"net"
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
	lnx_file := os.Args[1]

	// Create a new host node
	var stack protocol.IPStack
	stack.Initialize(lnx_file)
	for iface := range stack.Interfaces {
		go listen(stack, iface)
	}

	for {
		// REPL
		var userInput string
		fmt.Scanln(&userInput)

		if (userInput == "li") {
			fmt.Println(stack.Li())
		} else if (userInput == "ln") {
			fmt.Println(stack.Ln())
		} else if (userInput == "lr") {
			// TODO
		} else if (userInput == "down") {
			stack.Down()
			// TODO - iface name
		} else if (userInput == "up") {
			// TODO - iface name
		} else if (userInput == "send") {
			// TODO - get send fields
			stack.SendIP()
		} else {
			fmt.Println("Invalid command.")
			os.Exit(0)
		}
	}

}
