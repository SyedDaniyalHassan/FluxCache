package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/SyedDaniyalHassan/fluxcache/pkg/cluster"
	"github.com/SyedDaniyalHassan/fluxcache/pkg/monitoring"
)

// CacheItem represents a value in the cache with optional TTL and versioning
type CacheItem struct {
	Value       interface{} `json:"value"`
	Expiration  int64       `json:"expiration"`   // Unix timestamp, 0 means no expiration
	LastUpdated int64       `json:"last_updated"` // Unix timestamp (ms) of last update
}

// Cache is a thread-safe in-memory cache
type Cache struct {
	store sync.Map
}

var (
	cache        *Cache
	clust        *cluster.Cluster
	selfID       string
	replicaCount = 2
)

func NewCache() *Cache {
	return &Cache{}
}

func (c *Cache) Set(key string, value interface{}, ttlSeconds int64, lastUpdated int64) bool {
	exp := int64(0)
	if ttlSeconds > 0 {
		exp = time.Now().Add(time.Duration(ttlSeconds) * time.Second).Unix()
	}
	item := CacheItem{Value: value, Expiration: exp, LastUpdated: lastUpdated}
	for {
		v, loaded := c.store.Load(key)
		if !loaded {
			c.store.Store(key, item)
			return true
		}
		existing := v.(CacheItem)
		if lastUpdated >= existing.LastUpdated {
			c.store.Store(key, item)
			return true
		}
		// If incoming is older, do not update
		return false
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	v, ok := c.store.Load(key)
	if !ok {
		return nil, false
	}
	item := v.(CacheItem)
	if item.Expiration > 0 && time.Now().Unix() > item.Expiration {
		c.store.Delete(key)
		return nil, false
	}
	return item.Value, true
}

func (c *Cache) Delete(key string) {
	c.store.Delete(key)
}

func forwardRequest(node *cluster.Node, path string, method string, body []byte) (*http.Response, error) {
	url := "http://" + node.Addr + path
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	return client.Do(req)
}

func handleSet(cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key         string      `json:"key"`
			Value       interface{} `json:"value"`
			TTL         int64       `json:"ttl"`
			LastUpdated int64       `json:"last_updated"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		if req.LastUpdated == 0 {
			req.LastUpdated = time.Now().UnixNano() / int64(time.Millisecond)
		}
		replicas := clust.GetResponsibleNodes(req.Key, replicaCount)
		isReplica := false
		healthyReplicas := []*cluster.Node{}
		for _, node := range replicas {
			if clust.Health == nil || clust.Health.IsNodeHealthy(node.ID) {
				healthyReplicas = append(healthyReplicas, node)
			}
			if node.ID == selfID {
				isReplica = true
			}
		}
		if len(healthyReplicas) == 0 {
			http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
			return
		}
		if !isReplica {
			body, _ := json.Marshal(req)
			for _, node := range healthyReplicas {
				if node.ID != selfID {
					forwardRequest(node, "/set", "POST", body)
				}
			}
			w.WriteHeader(http.StatusNoContent)
			log.Printf("[SET] Node: %s, Key: %s, Replicas: %v, IsReplica: %v", selfID, req.Key, nodeIDs(replicas), isReplica)
			return
		}
		updated := cache.Set(req.Key, req.Value, req.TTL, req.LastUpdated)
		if !updated {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("conflict: incoming update is older than current value"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		log.Printf("[SET] Node: %s, Key: %s, Replicas: %v, IsReplica: %v", selfID, req.Key, nodeIDs(replicas), isReplica)
	}
}

func handleGet(cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		replicas := clust.GetResponsibleNodes(key, replicaCount)
		healthyReplicas := []*cluster.Node{}
		for _, node := range replicas {
			if clust.Health == nil || clust.Health.IsNodeHealthy(node.ID) {
				healthyReplicas = append(healthyReplicas, node)
			}
		}
		if len(healthyReplicas) == 0 {
			http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
			return
		}
		for _, node := range healthyReplicas {
			if node.ID == selfID {
				value, ok := cache.Get(key)
				if ok {
					resp := map[string]interface{}{"key": key, "value": value}
					if item, ok := cache.store.Load(key); ok {
						resp["last_updated"] = item.(CacheItem).LastUpdated
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(resp)
					return
				}
			} else {
				url := "http://" + node.Addr + "/get?key=" + key
				resp, err := http.Get(url)
				if err == nil && resp.StatusCode == http.StatusOK {
					defer resp.Body.Close()
					w.WriteHeader(resp.StatusCode)
					io.Copy(w, resp.Body)
					return
				}
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func handleDelete(cache *Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		replicas := clust.GetResponsibleNodes(key, replicaCount)
		isReplica := false
		healthyReplicas := []*cluster.Node{}
		for _, node := range replicas {
			if clust.Health == nil || clust.Health.IsNodeHealthy(node.ID) {
				healthyReplicas = append(healthyReplicas, node)
			}
			if node.ID == selfID {
				isReplica = true
			}
		}
		if len(healthyReplicas) == 0 {
			http.Error(w, "no healthy replicas", http.StatusServiceUnavailable)
			return
		}
		if !isReplica {
			for _, node := range healthyReplicas {
				if node.ID != selfID {
					url := "http://" + node.Addr + "/delete?key=" + key
					req, _ := http.NewRequest("DELETE", url, nil)
					http.DefaultClient.Do(req)
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		cache.Delete(key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// Cluster management endpoints
func handleNodes(w http.ResponseWriter, r *http.Request) {
	nodes := []string{}
	for id := range clust.Nodes {
		nodes = append(nodes, id)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"nodes": nodes, "self": selfID})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Heartbeat endpoint for node health
func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ALIVE"))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	selfID = os.Getenv("NODE_ID")
	if selfID == "" {
		selfID = "localhost:" + port
	}
	// Static node discovery from config or env
	nodesEnv := os.Getenv("NODES") // comma-separated list
	var nodeAddrs []string
	if nodesEnv != "" {
		nodeAddrs = append(nodeAddrs, splitAndTrim(nodesEnv)...)
	} else {
		nodeAddrs = []string{selfID}
	}
	cache = NewCache()
	clust = cluster.NewCluster(selfID, nodeAddrs, 100)
	clust.StartHealthMonitor(selfID, 2*time.Second)

	monitoring.InitMetrics()
	http.Handle("/metrics", monitoring.PrometheusHandler())
	http.HandleFunc("/set", monitoring.InstrumentHandler("set", handleSet(cache)))
	http.HandleFunc("/get", monitoring.InstrumentHandler("get", handleGet(cache)))
	http.HandleFunc("/delete", monitoring.InstrumentHandler("delete", handleDelete(cache)))
	http.HandleFunc("/nodes", handleNodes)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/heartbeat", handleHeartbeat)
	log.Printf("[FluxCache] Cache node %s running on :%s, cluster: %v", selfID, port, nodeAddrs)

	replicaEnv := os.Getenv("REPLICA_COUNT")
	if replicaEnv != "" {
		if rc, err := strconv.Atoi(replicaEnv); err == nil && rc > 0 {
			replicaCount = rc
		}
	}

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func splitAndTrim(s string) []string {
	var out []string
	for _, part := range bytes.Split([]byte(s), []byte{','}) {
		out = append(out, string(bytes.TrimSpace(part)))
	}
	return out
}

func nodeIDs(nodes []*cluster.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
