package main

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"tcp-tcp-team-pa/lnxconfig"
	protocol "tcp-tcp-team-pa/pkg"
	tcp_protocol "tcp-tcp-team-pa/tcp_pkg"
)

func listen(ipStack *protocol.IPStack, iface *protocol.Interface) {
	// Since this is a host, we only listen on one interface
	for {
		ipStack.Receive(ipStack.Interfaces[iface.IP])
	}
}

func getOnlyKey(m map[netip.Addr]*protocol.Interface) netip.Addr {
	for k := range m {
		return k
	}
	var empty_addr netip.Addr
	return empty_addr
}

func handleTCP() {

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
	// Create a new IP stack
	var ipStack *protocol.IPStack = &protocol.IPStack{}
	ipStack.Initialize(*lnxConfig)

	// Create a new TCP stack
	var tcpStack *tcp_protocol.TCPStack = &tcp_protocol.TCPStack{}
	tcpStack.Initialize(getOnlyKey(ipStack.Interfaces), ipStack)

	for _, iface := range ipStack.Interfaces {
		go listen(ipStack, iface)
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Enter command:")
	for scanner.Scan() {
		// REPL
		userInput := scanner.Text()

		if userInput == "li" {
			fmt.Println(ipStack.Li())

		} else if userInput == "ln" {
			fmt.Println(ipStack.Ln())

		} else if userInput == "lr" {
			fmt.Println(ipStack.Lr())

		} else if len(userInput) >= 6 && userInput[0:4] == "down" {
			interfaceName := userInput[5:]
			ipStack.Down(interfaceName)

		} else if len(userInput) >= 4 && userInput[0:2] == "up" {
			interfaceName := userInput[3:]
			ipStack.Up(interfaceName)

		} else if len(userInput) >= 6 && userInput[0:4] == "send" {
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

			ipStack.SendIP(nil, 32, destIP, 0, []byte(message))
		} else if userInput == "ls" {
			tcpStack.ListSockets()
		} else if len(userInput) == 7 && userInput[0:1] == "a" {
			port, _ := strconv.ParseUint(userInput[2:], 10, 16)
			tcpStack.ACommand(uint16(port))
		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}
