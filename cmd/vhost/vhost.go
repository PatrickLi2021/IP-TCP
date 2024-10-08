package main

import (
	"fmt"
	"ip-ip-pa/pkg/protocol"
	"log"
	"net"
	"os"
)

const max_packet_size = 1400

func listen(host *protocol.IPStack) {
	// Since this is a host, we only listen on one interface
	interface_port := host.interfaces[0].Udp
	addr, err := net.ResolveUDPAddr("udp4", interface_port)
	if err != nil {
		log.Panicln("Error resolving address:  ", err)
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		fmt.Println("Error creating connection")
		return
	}
	packetBuffer := make([]byte, max_packet_size)
	n, _, err := conn.ReadFromUDP(packetBuffer)
	if n > 0 {
		host.Receive()
	}

}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./vhost --config <lnx file>")
		return
	}
	lnx_file := os.Args[1]

	// Create a new host node
	var host protocol.IPStack
	host.Initialize(lnx_file)
	go listen(host)

}
