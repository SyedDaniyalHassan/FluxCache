package cluster

import (
	"crypto/sha1"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Node represents a cache node in the cluster
type Node struct {
	ID   string // Unique node ID (e.g., host:port)
	Addr string // Address for communication
}

// HashRing implements consistent hashing
type HashRing struct {
	replicas int
	nodes    []int          // Sorted hash ring
	nodeMap  map[int]string // hash -> nodeID
	mu       sync.RWMutex
}

func NewHashRing(replicas int) *HashRing {
	return &HashRing{
		replicas: replicas,
		nodeMap:  make(map[int]string),
	}
}

func (h *HashRing) AddNode(nodeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < h.replicas; i++ {
		hash := int(hashKey(fmt.Sprintf("%s#%d", nodeID, i)))
		h.nodes = append(h.nodes, hash)
		h.nodeMap[hash] = nodeID
	}
	sort.Ints(h.nodes)
}

func (h *HashRing) RemoveNode(nodeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	filtered := h.nodes[:0]
	for _, hash := range h.nodes {
		if h.nodeMap[hash] != nodeID {
			filtered = append(filtered, hash)
		} else {
			delete(h.nodeMap, hash)
		}
	}
	h.nodes = filtered
}

func (h *HashRing) GetNode(key string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.nodes) == 0 {
		return ""
	}
	hash := int(hashKey(key))
	idx := sort.Search(len(h.nodes), func(i int) bool { return h.nodes[i] >= hash })
	if idx == len(h.nodes) {
		idx = 0
	}
	return h.nodeMap[h.nodes[idx]]
}

func hashKey(key string) uint32 {
	h := sha1.New()
	h.Write([]byte(key))
	bs := h.Sum(nil)
	return (uint32(bs[16])<<24 | uint32(bs[17])<<16 | uint32(bs[18])<<8 | uint32(bs[19]))
}

// NodeHealthTracker tracks health of nodes
type NodeHealthTracker struct {
	status map[string]bool
	mu     sync.RWMutex
}

func NewNodeHealthTracker() *NodeHealthTracker {
	return &NodeHealthTracker{status: make(map[string]bool)}
}

func (n *NodeHealthTracker) IsNodeHealthy(nodeID string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	healthy, ok := n.status[nodeID]
	return ok && healthy
}

func (n *NodeHealthTracker) MarkNodeHealth(nodeID string, healthy bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.status[nodeID] = healthy
}

// StartHealthMonitor periodically checks /heartbeat for all nodes
func (c *Cluster) StartHealthMonitor(selfID string, interval time.Duration) {
	if c.Health == nil {
		c.Health = NewNodeHealthTracker()
	}
	go func() {
		for {
			c.mu.RLock()
			for id, node := range c.Nodes {
				if id == selfID {
					c.Health.MarkNodeHealth(id, true)
					continue
				}
				url := "http://" + node.Addr + "/heartbeat"
				client := &http.Client{Timeout: 0}
				resp, err := client.Get(url)
				if err == nil && resp.StatusCode == 200 {
					c.Health.MarkNodeHealth(id, true)
					resp.Body.Close()
				} else {
					c.Health.MarkNodeHealth(id, false)
				}
			}
			c.mu.RUnlock()
			time.Sleep(interval)
		}
	}()
}

// Cluster manages nodes and routing
type Cluster struct {
	Nodes    map[string]*Node
	HashRing *HashRing
	SelfID   string
	Health   *NodeHealthTracker
	mu       sync.RWMutex
}

func NewCluster(selfID string, nodeAddrs []string, replicas int) *Cluster {
	nodes := make(map[string]*Node)
	hashRing := NewHashRing(replicas)
	for _, addr := range nodeAddrs {
		node := &Node{ID: addr, Addr: addr}
		nodes[addr] = node
		hashRing.AddNode(addr)
	}
	return &Cluster{
		Nodes:    nodes,
		HashRing: hashRing,
		SelfID:   selfID,
	}
}

// GetResponsibleNode returns the node responsible for a key
func (c *Cluster) GetResponsibleNode(key string) *Node {
	nodeID := c.HashRing.GetNode(key)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Nodes[nodeID]
}

// GetReplicas returns the list of node IDs responsible for a key (primary + replicas)
func (h *HashRing) GetReplicas(key string, replicaCount int) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.nodes) == 0 || replicaCount <= 0 {
		return nil
	}
	hash := int(hashKey(key))
	idx := sort.Search(len(h.nodes), func(i int) bool { return h.nodes[i] >= hash })
	if idx == len(h.nodes) {
		idx = 0
	}
	seen := make(map[string]struct{})
	replicas := []string{}
	for i := 0; len(replicas) < replicaCount && i < len(h.nodes)*2; i++ {
		nodeID := h.nodeMap[h.nodes[(idx+i)%len(h.nodes)]]
		if _, ok := seen[nodeID]; !ok {
			replicas = append(replicas, nodeID)
			seen[nodeID] = struct{}{}
		}
	}
	return replicas
}

// GetResponsibleNodes returns the Node structs for the key's replicas
func (c *Cluster) GetResponsibleNodes(key string, replicaCount int) []*Node {
	nodeIDs := c.HashRing.GetReplicas(key, replicaCount)
	c.mu.RLock()
	defer c.mu.RUnlock()
	nodes := []*Node{}
	for _, id := range nodeIDs {
		if n, ok := c.Nodes[id]; ok {
			nodes = append(nodes, n)
		}
	}
	return nodes
}
