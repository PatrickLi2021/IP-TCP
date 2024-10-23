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

- `ipCostInterfaceTuple`: This struct represents the value entity of the key-value structure that represents our forwarding table for both routers and hosts. It contains a next hop IP address, a cost for reaching that next hop address, the interface associated with that next hop, a type for that route (representing how that route was learned), and a timestamp of when that route was las refreshed.

- `IPStack`: This struct is used to initialize an instance of either a host or router and provides extensible logic for both. It contains a routing type (none, static, or RIP), a forwarding table instance, a handler table to register handler functions, a map containing addresses to interfaces, a name-to-interface map, and a list of RIP neighbors (which is only used if the routing type of this node is RIP).

##### IP Stack API Functions
The primary responsibility of the IP stack API is to allow the user to initialize nodes as well as send and receive packets. The main functions that we include in our IP stack API are the following:

- `Initialize()`: For this function, we essentially are initializing an instance of an IP stack, whether it is a host or a router.  The function's only argument is a `lnxconfig.IPConfig` file. An empty `IPStack` instance is created and its fields are populated. Additionally, the handler functions that are executed when a packet is received are registered into the `IPStack` instance's handler table field.

- `SendIP()`: This function takes in a a source IP address, the current TTL of the packet, the destination IP address, the protocol number of that packet, and the data that is stored in that packet. We first check if the packet is being sent to itself. If it is, we can immediately execute the handler function. If it's not, we want to find the longest matching prefix in the current node's forwarding table. To do this, we created a helper function called `findPrefixMatch()` that takes in a destination `netip.Addr` and returns a source IP address and destination `netUDPAddr`. Next, we have error checking that ensures that the source interface is down or not. If it is down, then we want to drop the packet (note that we are still able to receive it though). From there, we construct the packet to send by creating an instance of a `ipv4header.IPv4Header`, marshaling the header bytes, and computing a checksum via a helper function. Lastly, the bytes of the payload of the packet are constructed and sent to the destination via `WriteToUDP()` .

- `findPrefixMatch()`:

- `Receive()`: In this function, we first start by creating a buffer to store the data we read in, thn call `ReadFromUDP`. We parse the header bytes into an `ipv4header.IPv4Header` and then validate the checksum and TTL of the incoming packet. At this point, if the current interface calling the `Receive()` method is the correct destination, then we want to construct a new packet (since all we received in the first place were bytes) and call the appropriate callback function based on the protocol number. If the packet has not reached the appropriate destination, then we call `SendIP()` with the updated destination.

#### Host (vhost)
Our host file consists of 2 main threads/goroutines: the **main** thread and the **listener** thread. The main thread is the initial point of entry and starts off by parsing the lnx configuration file. It then creates an instance of an `IPStack` and initializes its fields. From there, we iterate over all the interfaces of that node and starting listening on each one with a thread. The rest of the main function handles REPL commands from the user.

#### Router (vrouter)
The overall structure of our router file is the same as the host file. However, one thing that is different is that upon router initialization, we send a RIP request to all of the current router's neighbors. Additionally, we instantiate 2 additional goroutines in addition to the `listen()` goroutine: `routerPeriodicSend()` and `cleanExpiredRoutesTicker()`. The first thread's responsibility is to send router updates to RIP neighbors every 5 seconds and the second thread goes through every router in the router's forwarding table to clean up expired routes.

### Known Bugs
N/A