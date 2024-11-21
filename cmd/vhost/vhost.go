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
	emptyAddr, _ := netip.ParseAddr("0.0.0.0")
	return emptyAddr
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

	for _, iface := range ipStack.Interfaces {
		go listen(ipStack, iface)
	}

	// Create a new TCP stack
	var tcpStack *protocol.TCPStack = &protocol.TCPStack{}
	tcpStack.Initialize(getOnlyKey(ipStack.Interfaces), ipStack)

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
		} else if len(userInput) > 2 && userInput[0:2] == "a " {
			port, err := strconv.ParseUint(userInput[2:], 10, 16)
			if err != nil {
				fmt.Println(err)
				continue
			}
			go tcpStack.ACommand(uint16(port))
		} else if len(userInput) > 10 && userInput[0:2] == "c " {
			inputs := strings.Fields(userInput[2:])
			ip, err := netip.ParseAddr(inputs[0])
			if err != nil {
				fmt.Println(err)
				continue
			}
			port, err := strconv.ParseUint(inputs[1], 10, 16)
			if err != nil {
				fmt.Println(err)
				continue
			}
			tcpConn, err := tcpStack.VConnect(ip, uint16(port))
			if err != nil {
				fmt.Println(err)
				continue
			}
			<-tcpConn.SfRfEstablished
			// thread that is woken up when there is stuff in send buf to send out
			go tcpConn.CheckRTOTimer()
			go tcpConn.SendSegment()
		} else if len(userInput) >= 5 && userInput[0:2] == "s " {
			parts := strings.Split(userInput, " ")
			socketID, _ := strconv.Atoi(parts[1])
			bytesToSend := strings.Join(parts[2:], " ")
			tcpStack.SCommand(uint32(socketID), bytesToSend)
		} else if len(userInput) >= 5 && userInput[0:2] == "r " {
			parts := strings.Split(userInput, " ")
			socketID, _ := strconv.Atoi(parts[1])
			numBytesToRead, _ := strconv.ParseUint(parts[2], 10, 32)
			tcpStack.RCommand(uint32(socketID), uint32(numBytesToRead))
		} else if len(userInput) > 8 && userInput[0:2] == "sf" {
			parts := strings.Fields(userInput)
			filePath := parts[1]
			addr, _ := netip.ParseAddr(parts[2])
			portInt, _ := strconv.Atoi(parts[3])
			port := uint16(portInt)
			tcpStack.SfCommand(filePath, addr, port)
		} else if len(userInput) > 6 && userInput[0:2] == "rf" {
			parts := strings.Fields(userInput)
			filePath := parts[1]
			portInt, _ := strconv.Atoi(parts[2])
			port := uint16(portInt)
			tcpStack.RfCommand(filePath, port)
		} else if len(userInput) >= 4 && userInput[0:2] == "cl" {
			parsedUint, _ := strconv.ParseUint(userInput[3:], 10, 16)
			socketId := uint16(parsedUint)
			tcpStack.CloseCommand(socketId)
		} else {
			fmt.Println("Invalid command.")
			continue
		}
	}
}
