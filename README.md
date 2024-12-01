# IP Design

## High-Level Design
To design IP, we broke up our project into several different components: `cmd` and `pkg`. The `cmd` folder contains our logic and files for the host and router nodes in `vhost.go` and `vrouter.go` respectively. The `pkg` folder contains many different files, including one file with the handler functions (`handler.go`), one file with the IP node API (`protocol.go`), one file with the REPL logic (`repl.go`), one file called `util.go` that holds functions for converting between a `uint32` and `netip.Addr`, and lastly one file called `rip.go` that handles the RIP protocol logic.
 
## Application Flow
In order to run the application, you have to first select the topology you want to run. Each network topology has a set of hosts or routers as well as how they're connected. Each host only has 1 interface to listen for packets on and a router can have multiple interfaces. 

Next, you have to generate the corresponding `lnx` files for each node. These files tell each node how to connect to the network at startup (i.e. its interfaces, IP addresses, local network configuration, etc.)

From here, you can load up the network and send packets from node to node.

## Application Structure and Notable Design Decisions

### IP Layer
In terms of constructing our network layer, as mentioned previously we have an `IPStack` API that provides the core functionality that is implemented by both hosts and routers.

#### IP Stack API

##### IP Stack API Data Entities
The primary data entities/structs that are contained within our IP API protocol are the following:

- `IPPacket`: This struct holds the data needed to construct an IP packet. We use the built-in Go `ipv4header.Ipv4Header` type to represent the header and the payload is represented as a byte array.
 
- `Interface`: This struct holds the relevant fields for an interface of a particular router or host. Each interface has a name, IP address, network submask/prefix, neighbors map, UDP address of the interface, an up/down status, a `net.UDPConn` instance to listen to incoming UDP packets, and a mutex to handle concurrent reads and writes.

- `ipCostInterfaceTuple`: This struct represents the value entity of the key-value structure stored in the forwarding table for both routers and hosts. It contains a next hop IP address, a cost for reaching that next hop address, the interface associated with that next hop, a type for that route (representing how that route was learned), and a timestamp of when that route was last refreshed.

- `IPStack`: This struct is used to initialize an instance of either a host or router and provides extensible logic for both. It contains a routing type (none, static, or RIP), a forwarding table instance, a handler table to register handler functions, a map containing addresses to interfaces, a name-to-interface map, and a list of RIP neighbors (which is only used if the routing type of this node is RIP).
The IPStack API functions are called on this struct. 

##### IP Stack API Functions
The primary responsibility of the IP stack API is to allow the user to initialize nodes as well as send and receive packets. The main functions that we include in our IP stack API are the following:

- `Initialize()`: For this function, we essentially are initializing an instance of an IP stack, whether it is a host or a router.  The function's only argument is a `lnxconfig.IPConfig` file. The function is called on an uninitialized `IPStack` instance to populate its fields. Specifically, the function will register necessary handler functions, create the interfaces on the node to store in the IPStack fields, populate its neighbors map, and populate its forwarding table with the interfaces. If the routing is RIP, this will also set the struct's RipNeighbors.  

- `SendIP()`: This function takes in a a source IP address, the current TTL of the packet, the destination IP address, the protocol number of that packet, and the data that is stored in that packet. The general flow for this function is to find the longest prefix match through a helper function called `findPrefixMatch()` that takes in a destination `netip.Addr` and returns a source IP address and destination `netUDPAddr`. Next, we have error checking that will drop the packet if no match is returned from `findPrefixMatch()`. We also check if the source interface the packet is sent from is down. If it is down, then we want to drop the packet (note that this allows the packet to still be sent along its typical path until it reaches the interface that is down - adhering to the reference behavior). From there, we construct the packet to send by creating an instance of a `ipv4header.IPv4Header`, marshaling the header bytes and computing a checksum via a helper function. Lastly, the bytes of the payload of the packet are constructed and sent to the destination via `WriteToUDP()` .

- `findPrefixMatch()`: This function is called on an IPStack struct and takes a destination IP address as an argument. The function starts by looping through all prefix and tuple key-value pairs in the stack's forward table to determine the longest prefix match of the destination IP. If no match is found, we return nil. If the resulting tuple has route type of "L", which is a default route, we must make one additional lookup in the table by calling `findPrefixMatch()`, passing in the resulting tuple's IP as the new destination IP argument. Otherwise, we loop through all of the resulting tuple's interface's neighbors to see if any neighbor's IP matches the destination IP. If so, we return the neighbor's IP and udpAddr. If not, no match was found and nil is returned. 

- `Receive()`: In this function, we first start by creating a buffer to store the data we read in, then call `ReadFromUDP`. We parse the header bytes into an `ipv4header.IPv4Header` and then validate the checksum and TTL of the incoming packet. At this point, if the current interface calling the `Receive()` method is the correct destination, then we want to construct a new packet (since all we received in the first place were bytes) and call the appropriate callback function based on the protocol number. If the packet has not reached the appropriate destination, then we call `SendIP()` with the updated destination.

#### Host (vhost)
Our host file consists of 2 main threads/goroutines: the **main** thread and the **listener** thread. The main thread is the initial point of entry and starts off by parsing the lnx configuration file. It then creates an instance of an `IPStack` and initializes its fields. From there, we iterate over all the interfaces of that node and starting listening on each one with a thread. The rest of the main function handles REPL commands from the user in a continuous for loop.
On each of the interface listen threads, we simply call our IPStack's `Receive()` function in a continuous for loop. 

#### Router (vrouter)
The overall structure of our router file is the same as the host file. However, one thing that is different is upon router initialization, we send a RIP request to all of the current router's neighbors. Additionally, we instantiate 2 additional goroutines in addition to the `listen()` goroutine on every interface: `routerPeriodicSend()` and `cleanExpiredRoutesTicker()`. The first thread's responsibility is to send router updates to RIP neighbors every 5 seconds and the second thread goes through every router in the router's forwarding table to clean up expired routes.

### Known Bugs
Nothing we are aware of. 


# TCP Design

## Connection Structs
First, we implemented a TCPStack struct, which most notably has fields ListenTable, ConnectionsTable, and IP. ListenTable maps a port to a struct representing a listener socket. The ConnectionsTable struct maps a four tuple (remote port, remote address, source port, source address) to a struct representing a normal socket.

Then to represent a connection, we created 2 distinct structs: one for a listener socket and the other for a normal socket.

### TCPListener
- The first struct we created was the TCPListener struct to represent a listener socket. 
- This struct was simple and is comprised of state variables, such as Local Port, Local Addr, etc. Most importantly was the field for TCPStack, which holds the ListenTable and ConnectionsTable for the listener socket to look up incoming connections or create a new normal connection.

### TCPConn
- The second struct we created was the TCPConn struct to represent a normal socket.
- This struct has state fields, such as LocalPort, LocalAdr, SeqNum, ACK, ISN (initial seq num), CurWindow, ReceiverWin, etc. These fields all help the conn track information needed to communicate with the other connection
- Some more important fields include the SendBuf and RecvBuf, which are used to track what information to send and receive. We implemented our own circular buffer from a byte slice, where the pointers are 0-indexed and are not-relative to the indices in the buffer. The pointers grow and when we index into the buffer, we will mod by the buffer-size. 
- Next, we use a map from seq num to early arrival packet data to track our early arrivals. 
- Lastly, we use an RTStruct to track retransmissions.

### RTStruct
- The RTStruct has several state fields, such as SRTT, Alpha, Beta, and RTO that are used in calculating the new value for the RTOTimer, which is a time.Ticker.
- To actually track the retransmissions to be re-sent, we use a slice of *RTPacket. 

### RTPacket
- The RTPacket represents one packet that needs to be retransmitted.
- The fields include a Timestamp, SeqNum, AckNum, Data, Flags, and NumTries, which tracks how many times we have already retransmitted the packet.

## Threads

### Sending/Receiving
- To send or whenever a new connection is established, we first start a thread called CheckRTOTimer(), which will retransmit packets (if any) when the timer expires. Secondly, we start a thread called SendSegment(), which loops forever but uses a channel to block until there is data in the conn's send buffer. Data will be present in the send buffer is the user tries to send, which calls a function VWrite to write into the send buffer. 
- Receiving data is part of the TCPHandler function that is called through our IPStack when a packet is at its destination. We have registered this handler function when our IPStack is initialized. When data reaches the destination, we write this data into the connection's receive buffer. To read from the receiver buffer, we call VRead, which will adjust the buffer pointers and return the bytes read.
- Slight exceptions to this occur with the sf and rf commands. The rf command triggers an RFCommand() thread, which will call VRead and write the data to a file in a for loop until the other connection initiates a close. The sf command also triggers an SFCommand() thread, which will start the SendSegment() thread and continuously call VWrite in a for loop until it has written all file contents. 

### Retransmissions
- The only extra thread created is called CheckRTOTimer, which retransmits packets (if any) when the timer expires. 
- The remaining, necessary retransmission logic is handled throughout the rest of the TCP Code. For instance, in SendSegment() we will add any packets sent to the retranmission queue and restart the timer. Additionally, within TCPHandler, we call a function to remove ACK'd packets from the retransmission queue.

### Early Arrivals
- We don't add any additional threads for early arrivals. The logic is again incorporated into our TCP Code. Within TCPHandler, we check the incoming packet's seq number against our next expected seq num. If it is an early arrival we add it to our map. When we do receive what we expect, we consult the early arrivals map to reconstruct as much data as we can, write it into the receive buffer, and delete it from the early arrivals map.

## Improvements
- If we could redo this assignment again, we would probably use a sync map or other packages in Go that could make our code concurrency-safe because it might be easier than adding manual locks. We think something that slowed our performance down was how we loop through retransmissions when deleting them from the queue. Instead, it might have been easier to utilize a data structure like a priority queue instead of a slice, where the lowest sequence number would have highest priority. 

## Bugs
- To the best of our knowledge (we ran the code so many times on files >= 1 MB before calling the project done), we did not see any bugs before submitting.
- It is possible that since we have no manual locking, that there could be concurrency bugs? Although we have never run into this issue before. 

## Performance Measurements
We sent a file of size 1,189,921 bytes. The reference sent the file in 0.07698 seconds. Our implementation sent the file in 7.05259 seconds.

## Packet Capture
### 3-way Handshake: 
- Packet capture lines: 1, 2, 3
- Implementation is responding appropriately

### One Segment Sent and Acknowledged
- Packet capture line (sent): 4
- Packet capture line (acknowledged): 13
- Implementation is responding appropriately

### Retransmission
- Packet capture line: 118
- Implementation is responding appropriately

### Connection Teardown
- Packet capture lines: 1766, 1787, 1788, 1789