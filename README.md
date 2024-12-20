# IP-TCP

This project involves 2 major components: the **IP stack** and the **TCP stack**.

## IP

This part of the project involves constructing a **virtual IP network** that implements a link layer, IP forwarding, and routing. The project mimics the functionality of a network stack typically provided by the operating system, drivers, and hardware of a host or router. The virtual network is built entirely in user-space programs.

### Overview

The virtual IP network simulates a network stack by constructing 2 primary components:
1. **IP Forwarding:** The IP forwarding component processes received packets and determines whether to deliver them locally or forward them to another interface.
2. **IP Stack API:** The IP stack API provides an interface for sending and receiving packets. It enables the seamless integration of the TCP stack and is used by both hosts and routers in the network.
3. **RIP Routing:** This component implements the _routing information protocol_ to exchange routing information and dynamically update a router's forwarding table

### Node Representation
- **vhost:** The `vhost` program represents a **host** or **node** in the network. It uses the IP stack to send and receive UDP packets as well as communicate with other hosts and routers on the network.
- **vrouter:** The `vrouter` program represents a **router** in the network. In addition to the features of `vhost`, it implements the RIP protocol and maintains and updates the forwarding table based on routing information received from neighboring routers.

### Virtual Network Design

The IP stack is built in a _virtual_ network. Instead of dealing with real hardware or drivers in the kernel, as mentioned previously, the IP stack is built entirely in users-space programs will make up hosts and routers. Here is an example network topology that we can create:

<img width="297" alt="Screenshot 2024-12-19 at 5 23 30 PM" src="https://github.com/user-attachments/assets/20a5507f-ba6a-4ce2-9e8d-e3bcc5163f74" />

This network has 3 hosts and 2 routers. To run this network, we define the topology using _configuration files_.

#### Configuration Files

We set up our network from a set of configuration files that define how the nodes connect to each other, what interfaces they have, and their IP addresses. There are 2 types of configuration files: **network definition files** and **lnx files**.

- **Network Definition Files (`network-name.json`):** These files define the network topology, including how many nodes are in each network, number of hosts, number of routers, and how they are connected together. The nodes that are defined will read the lnx files to determine their network settings.
  
- **Lnx Files (`<node name>.lnx`):** These files define the network settings for a host or router - they will be read at startup to initialize the IP stack by creating interfaces, assigning IP addresses, and so on. These files are used to populate your node's forwarding table, and for routers, determine your initial configuration for RIP.

#### Interfaces and IPs

Each node has one or more _interfaces_, which are defined by the lnx file passed in at startup. Our interfaces will be simulated using UDP sockets: each interface has its own UDP port where it can send/receive packets: sending a packet from this UDP port is equivalent to sending the packet from that interface. For each interface, each node's UDP port is similar to a MAC address, which is a unique value (within our network) to identify that interface.

Each node's interfaces are defined by the `interface` directive in its lnx file. For example, here are `r2`'s interface definitions from the example above.

<img width="581" alt="Screenshot 2024-12-20 at 1 03 12 PM" src="https://github.com/user-attachments/assets/7f8a8548-d2fe-4668-94de-de2d8ff57166" />

#### Virtual IPs
Just like any _real_ IP network interface, a virtual interface has a virtual interface has a virtual IP address, netmask, and other settings for how to communicate with hosts on the local network. These IP addresses and networks do not route to the Internet - they exist solely within our virtual network.

Following the the example above, `r2` is a member of 2 virtual IP networks:
- `10.1.0.0/24` with virtual IP `10.1.0.2` (shared with `r1`)
- `10.2.0.0/24` with virtual IP `10.2.0.1` (shared with hosts `h2` and `h3`)

#### Neighbors and Local Networks
In this virtual network, an interface is connected to one or more other nodes on a single IP subnet (eg. 10.1.0.0/24). All nodes on the same subnet can always communicate with each other, and always know each other’s IP addresses and “link-layer” UDP port information—this is provided in the node’s lnx file using the neighbor directive.

For example, for each of `r2`’s subnets, there is a neighbor directive to tell `r2` about each other host on the network, as follows:

<img width="573" alt="Screenshot 2024-12-20 at 3 56 00 PM" src="https://github.com/user-attachments/assets/5097b229-8a18-492e-ad3f-27ee17fb5ac1" />

This means that `r2` (and any node: host or router) will always know how to reach its neighbors connected to the same subnet. For example, `r2` always knows that there are 2 neighbors reachable from interface `if1` (which has prefix `10.2.0.1/24`):

`10.2.0.3` at `127.0.0.1:5005`
`10.2.0.2` at `127.0.0.1:5006`

### The IP Stack
The core of the IP stack will be a virtual link layer and network layer, which together make up a framework to send and receive IP packest over the virtual network. A representation for a node's virtual interfaces (which are UDP sockets) and API for how to send and receive packets across the links is created here.

This figure below shows the overall architecture for the major components:

<img width="717" alt="Screenshot 2024-12-20 at 4 04 48 PM" src="https://github.com/user-attachments/assets/ab816d69-8b54-42ba-b4a0-45f5100cdda1" />

When your nodes start up, you will listen for packets on each interface and send them to your virtual network layer for processing—you will parse the packet, determine if it’s valid, and then decide what to do with it based on the forwarding table. Routers have multiple interfaces and can forward packets to another interface. In our network, hosts have only one interface and only send or receive packets.

From there, the IP stack is used to send and receive packets over the virtual network. At this stage, we have two "higher layers."

- **Test Packets** (both hosts and routers): A simple interface for sending packets from the command line on each host/router.
- **RIP** (routers only): Routers will communicate with each other to build a global view of all networks in the system and adapt to network changes.

Both hosts and routers will have a small command line interface send packets, gather information about your network stack, and enable/disable interfaces

#### IP-in-UDP Encapsulation
UDP is used as the link layer for this project. Each node will create an interface for every line in its links file - those interfaces are implemented by a UDP socket. All of the virtual link layer frames it sends are directly encapsulated as payloads of UDP packets that will be sent over these sockets.

#### IP Forwarding
In addition, this project features a network layer that sends and receives IP packets using your link layer. Overall, the network layer will read packets from the link layer, then decide what to do with the packet: deliver it locally delivery or forward it to the next hop destination. The IP forwarding process follows the **IPv4 protocol** described in RFC791.

The IP forwarding feature of this project performs _longest prefix matching_ in order to handle cases where there are multiple matches for a destination address. Additionally, the packets use the standard IPv4 header format described in Section 3.1 of RFC 791. The packet's TTL value is decremented and the checksum is recomputed.

### Routing Information Protocol (RIP) Specification

The **Routing Information Protocol (RIP)** is a widely-used distance-vector routing protocol designed to facilitate dynamic route discovery and dissemination among routers. RIP exchanges routing information with neighbors to construct and maintain routing tables, enabling efficient packet forwarding across interconnected networks. This specification outlines the customized version of RIP used in the Virtual IP Network project, adhering to the structure and behavior defined in RFC2453 with modifications tailored to the project's requirements.

#### RIP Message Format
The customized RIP protocol uses a slightly modified packet structure for exchanging routing information. Each RIP packet contains a command field, which specifies the type of message (1 for a request and 2 for a response), and a num_entries field that indicates the number of routing entries included in the packet, with a maximum value of 64. Each entry in the packet contains three fields: cost, which is an integer value representing the number of hops (ranging from 1 to 16, where 16 indicates infinity); address, which is the byte representation of the IP address for the advertised network; and mask, which is the subnet mask in byte format. For instance, to advertise the prefix 1.2.3.0/24, the address would be 1.2.3.0, and the mask would be 255.255.255.0. 

All fields must be transmitted in network byte order (big-endian). Unlike the standard RIP implementation, this version sends RIP packets encapsulated directly in virtual IP packets, using protocol number 200.

#### Protocol Operation
RIP operates as a distance-vector protocol where routers exchange routing information with their direct neighbors, referred to as RIP neighbors. In this virtual network, RIP neighbors are explicitly defined in .lnx configuration files using the rip advertise-to directive. When a router comes online, it immediately sends a RIP Request message to each of its neighbors, requesting their routing tables. Neighbors respond with a RIP Response, which contains all routes in their tables. 

Beyond this, RIP responses (or updates) are sent periodically every five seconds and whenever a router’s routing table changes. Periodic updates contain the entire routing table, whereas triggered updates, which occur due to table changes, include only the updated entries.

#### Split Horizon and Poison Reverse
To maintain stability and prevent routing loops, the protocol employs the split horizon mechanism with poisoned reverse. Split horizon ensures that a router does not send routing information back to the neighbor from which it originally learned the route. 

For example, if Router A learns about a route to Router C from Router B, it will not advertise this route back to Router B. With poisoned reverse, the router instead advertises the route with a cost of 16 (infinity), signaling the neighbor that the route is unreachable. This modification ensures more robust convergence and prevents persistent routing loops.

#### Route Timeouts
Routing table entries learned from neighboring routers are subject to expiration if they are not refreshed within 12 seconds. When a route expires, the router marks its cost as infinity (16) and sends a triggered update to inform neighbors of the change. Afterward, the expired route is removed from the routing table unless a new, better route is received. This ensures that the routing table is constantly updated with valid routes while discarding stale or unreachable ones.

#### Disabling Interfaces (Up/Down)
No specific changes to RIP functionality are required when network interfaces are disabled. However, routers may choose to optimize behavior by either ceasing to advertise routes associated with the disabled interface or advertising them with a cost of 16 to indicate unreachability. Additionally, a router may send a triggered update when a link is disabled or re-enabled, allowing its neighbors to adjust their routing tables more quickly. These optimizations are optional but can enhance network convergence and reduce unnecessary routing updates.

## TCP

This part of the project implements an RFC-compliant version of TCP on top of the virtual IP layer. Doing so allows us to extend the virtual network stack to implement _sockets_, which are the key networking abstraction at the transport layer that allows hosts to keep track of multiple, simultaneous connections.

TCP involves extending `vhost` to use TCP to reliably send data between hosts. The goal is to reliably send whole files between hosts, even under a lossy network that intentionally drops packets.

There are four major components to this part of the project: 
1. An abstraction for sockets which maps packets to individual connections. A socket API was created to work with sockets, similar to a real networking API that allows applications to use this socket representation to interact with the virtual network.
2. An implementation of the TCP state machine that implements connection setup and teardown.
3. The sliding window protocol that determines when to send and receive data, acknowledges received data, and retransmits data as necessary
4. A new set of REPL commands that allows the API to send and receive files.

The first 3 items make up the **TCP stack**, which is another "layer" in our node that implements TCP.

<img width="759" alt="Screenshot 2024-12-20 at 4 42 14 PM" src="https://github.com/user-
  
  attachments/assets/28480426-73bb-4973-884c-2f5c545ccd0b" />

Fundamentally, the TCP stack is another component in a host that receives packets from the IP layer, similar to how RIP and test packets were handled in the IP stack. TCP uses IP protocol number 6 to receive packets. The socket API is the interface between the REPL commands and the rest of the TCP stack.

### TCP State Machine
This part of the project implements the TCP state machine, which is required to manage the various states of a connection. A state diagram provides a representation of these states and transitions. While the diagram may initially appear complex, participants are advised to begin by focusing on connection setup and termination under ideal conditions.

The project follows the state machine and features described in RFC9293, with several exceptions:

- Simultaneous OPEN and CLOSE are not required.
- Silly Window Syndrome (SWS) avoidance is excluded.
- The implementation does not consider PSH or URG flags. TCP options, precedence, and congestion control are not included (except for capstone participants).
- Edge cases involving RST packets are only considered for normal sockets where an RST packet immediately terminates the connection.

Here is a diagram of the full TCP state machine that is implemented here:

<img width="740" alt="Screenshot 2024-12-20 at 4 58 12 PM" src="https://github.com/user-attachments/assets/4783948e-1f7f-4c3b-afbc-39ac04cfc800" />

### Building and Sending TCP Packets

#### Header and Libraries
All TCP packets must use the standard TCP header. However, participants are not required to manually construct or parse these headers. Libraries such as Google’s netstack can be utilized to serialize, deserialize, and compute checksums. The TCP-in-IP example demonstrates how to parse headers and calculate checksums.

Here are the key features for packets:

- **Initial Sequence Numbers (ISNs):** Participants may select a random 32-bit integer for the ISN, as suggested by RFC9293.
- **TCP Checksum:** The checksum calculation follows the pseudo-header method described in Section 3.1 of RFC9293.
- **Packet Size:** Packets do not not exceed the maximum MTU of 1400 bytes. The maximum TCP payload size is determined by subtracting the sizes of the IP and TCP headers from the MTU.
- **Options:** TCP options in incoming packets are be ignored, but this implementation does not crash when encountering such options.

#### Sliding Window Protocol
The sliding window protocol governs the transmission and reception of data and is a critical component of the TCP stack. The receiver handles out-of-order packets by placing them in a queue for later processing when the window catches up to their sequence numbers. The handling of these packets aligns with the relevant sections of RFC9293 and RFC1122.

#### Retransmissions and Round-Trip Time (RTT)
TCP retransmits timed-out segments based on the estimated round-trip time (RTT) to the destination. The RTT estimation includes the smoothed RTT (SRTT) and RTT variance (RTTVAR) according to the guidelines in RFC9293 Section 3 for measuring RTT, calculating timeouts, and managing retransmissions.

#### Connection Termination
Connection termination requires transitioning through specific states in the TCP state machine. All remaining data is transmitted and acknowledged before completing the termination process. The standard termination sequence described in RFC9293 applies, with modifications to simplify the project environment.
