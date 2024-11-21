package priorityQueue

import (
	"container/heap"
)

// An Item is something we manage in a priority queue.
type EarlyArrivalPacket struct {
	SeqNum     uint32 // The value of the item
	Index      int    // The index of the item in the heap
	PacketData []byte
}

// A PriorityQueue implements heap.Interface and holds Items.
type PriorityQueue []*EarlyArrivalPacket

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// We want Pop to give us the lowest, not highest, priority so we use less than here
	return pq[i].SeqNum < pq[j].SeqNum
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *PriorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*EarlyArrivalPacket)
	item.Index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // don't stop the GC from reclaiming the item eventually
	item.Index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

// update modifies the priority and value of an Item in the queue.
func (pq *PriorityQueue) update(item *EarlyArrivalPacket, seqNum uint32) {
	item.SeqNum = seqNum
	heap.Fix(pq, item.Index)
}
