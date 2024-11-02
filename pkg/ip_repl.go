package protocol

import "strconv"

// REPL commands
func (stack *IPStack) Li() string {
	var res = "Name Addr/Prefix  State"
	for _, iface := range stack.Interfaces {
		res += "\n" + iface.Name + "  " + iface.IP.String() + "/" + strconv.Itoa(iface.Prefix.Bits())
		if iface.Down {
			res += "  down"
		} else {
			res += "  up"
		}
	}
	return res
}

func (stack *IPStack) Ln() string {
	var res = "Iface VIP        UDPAddr"
	for _, iface := range stack.Interfaces {
		if iface.Down {
			continue
		} else {
			for neighborIp, neighborAddrPort := range iface.Neighbors {
				res += "\n" + iface.Name + "   " + neighborIp.String() + "   " + neighborAddrPort.String()
			}
		}
	}
	return res
}

func (stack *IPStack) Lr() string {
	var res = "T     Prefix       Next hop    Cost"
	stack.Mutex.RLock()
	for prefix, ipCostIfaceTuple := range stack.Forward_table {
		cost_string := strconv.FormatUint(uint64(ipCostIfaceTuple.Cost), 10)
		nextHopStr := ipCostIfaceTuple.NextHopIP.String()
		if (ipCostIfaceTuple.Type == "S") {
			cost_string = "-"
		}
		if (ipCostIfaceTuple.Type == "L") {
			nextHopStr = "LOCAL:" + stack.Interfaces[ipCostIfaceTuple.NextHopIP].Name
		}
		res += "\n" + ipCostIfaceTuple.Type + "     " + prefix.String() + "  " + nextHopStr + "   " + cost_string
	}
	stack.Mutex.RUnlock()
	return res
}

func (stack *IPStack) Down(interfaceName string) {
	// Set down flag in interface to true
	iface, exists := stack.NameToInterface[interfaceName]
	if exists {
		iface.Down = true
	}
}

func (stack *IPStack) Up(interfaceName string) {
	iface, exists := stack.NameToInterface[interfaceName]
	if exists {
		iface.Down = false
	}
}