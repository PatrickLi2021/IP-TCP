package main

import (
	"fmt"
	ipv4header "github.com/brown-csci1680/iptcp-headers"
	"net"
	"net/netip"
	"sync"
)

type HandlerFunc = func(*IPPacket, []interface{})

type Node int

const (
	Router = 0
	Host   = 1
)

type IPPacket struct {
	Header  ipv4header.IPv4Header
	Payload []byte
}

type Interface struct {
	Name   string       // the name of the interface
	IP     netip.Addr   // the IP address of the interface on this host
	Prefix netip.Prefix // the network submask/prefix
	Udp    net.UDPAddr  // the UDP address of the interface on this host
	Down   bool         // whether the interface is down or not
	Conn   *net.UDPConn // listen to incoming UDP packets
}

type IPStack struct {
	NodeType      Node
	Forward_table map[netip.Prefix][]interface{} // maps IP prefixes to interfaces or routing data
	Handler_table map[int]HandlerFunc            // maps protocol numbers to handlers
	Neighbors     map[netip.Addr]Interface       // maps (virtual) IPs to Interfaces
	Interfaces    map[string]*Interface          // maps interface names to interfaces
	Ip            netip.Addr                     // the IP address of this node
	Mutex         sync.Mutex                     // for concurrency
}

func (stack *IPStack) Initialize(configInfo IpConfig, nodeType Node) error {
	// Populate fields of stack (except node type)
	stack.NodeType = nodeType
	for i := 0; i < 5; i++ {
		fmt.Println("Iteration:", i)
	}

	// Go through each interface to populate list of interfaces for IPStack struct
	for lnxInterface := range configInfo.Interfaces {
		newInterface := Interface{
			Name:   lnxInterface.Name,
			IP:     lnxInterface.AssignedIP,
			Prefix: lnxInterface.AssignedPrefix,
			Udp:    lnxInterface.UDPAddr,
			Down:   false,
		}
		serverAddr, err := net.ResolveUDPAddr("udp4", lnxInterface.UDPAddr)
		if err != nil {
			fmt.Println(err)
		}
		conn, err := net.DialUDP("udp4", nil, serverAddr)
		if err != nil {
			fmt.Println(err)
		}
		newInterface.Conn = conn
		// Map interface name to interface struct
		stack.Interfaces[newInterface.Name] = &newInterface
	}

	// Go through each neighbor and populate a list of neighbors for IPStack struct
	interfaceNeighbors := []*Interface{}
	for neighbor := range configInfo.Neighbors {
		_, exists := stack.Interfaces[neighbor.InterfaceName]
		if exists {
			interfaceNeighbors = append(interfaceNeighbors, stack.Interfaces[neighbor.interfaceName])
		}
	}

}
