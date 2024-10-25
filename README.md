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