# IP-TCP

This project involves

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
  
- **Lnx Files (`<node name>.lnx`):** These files define the network settings for a host or router - they will be read at startup to initialize the IP stack by creating interfaces, assigning IP addresses, and so on. These files are used to populate your node's forwarding table, and for routers, determine your
