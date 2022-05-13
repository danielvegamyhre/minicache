package cache_client

import (
	"fmt"
	"bytes"
	"log"
	"errors"
	"net/http"
	"encoding/json"
	"io/ioutil"
	"crypto/tls"
	"crypto/x509"
	"context"
	"time"
	nodelib "github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/ring"
	"github.com/malwaredllc/minicache/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"	
)

type Payload struct {
	Key 	string		`json:"key"`
	Value	string		`json:"value"`
}


// Client wrapper for interacting with Identity Server
type ClientWrapper struct {
	Config 		nodelib.NodesConfig
	Ring		*ring.Ring
}

// Checks cluster config every 5 seconds and updates ring with any changes. Runs in infinite loop.
func (c *ClientWrapper) StartClusterConfigWatcher() {
	go func(){
		for {
			// ask random nodes who the leader until we get a response. 
			// we want each client to ask random nodes, not fixed, to avoid overloading one with leader requests
			var leader *nodelib.Node
			attempted := make(map[string]bool)
			for {
				randnode := nodelib.GetRandomNode(c.Ring.Nodes)

				// skip attempted nodes
				if _, ok := attempted[randnode.Id]; ok {
					log.Printf("Skipping visited node %s...", randnode.Id)
					continue
				}

				// mark visited and request leader
				log.Printf("Attempting to get leader from node %s", randnode.Id)
				attempted[randnode.Id] = true

				if randnode.GrpcClient == nil {
					c, err := NewCacheClient(randnode.Host, int(randnode.GrpcPort))
					if err != nil {
						log.Printf("error: %v", err)
						return
					}
					randnode.SetGrpcClient(c)
				}

				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				res, err := randnode.GrpcClient.GetLeader(ctx, &pb.LeaderRequest{Caller: "client"})
				if err != nil {
					continue
				}

				log.Printf("Found leader: %s", res.Id)
				leader = c.Config.Nodes[res.Id]
				break
			}


			// get cluster config from current leader
			req := pb.ClusterConfigRequest{CallerNodeId: "client"}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			res, err := leader.GrpcClient.GetClusterConfig(ctx, &req)
			if err != nil {
				log.Printf("error getting cluster config from node %s: %v", leader.Id, err)
				continue
			}

			// create hashmap of online node IDs to find missing node in constant time
			cluster_nodes := make(map[string]bool)
			for _, nodecfg := range res.Nodes {
				cluster_nodes[nodecfg.Id] = true
			}

			log.Printf("cluster config: %v", res.Nodes)

			// remove missing nodes from ring
			for _, node := range c.Config.Nodes {
				if _, ok := cluster_nodes[node.Id]; !ok {
					log.Printf("Removing node %s from ring", node.Id)
					delete(c.Config.Nodes, node.Id)
					c.Ring.RemoveNode(node.Id)
				}
			}

			// add new nodes to ring
			for _, nodecfg := range res.Nodes {
				if _, ok := c.Config.Nodes[nodecfg.Id]; !ok {
					log.Printf("Adding node %s to ring", nodecfg.Id)
					c.Config.Nodes[nodecfg.Id] = nodelib.NewNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
					c.Ring.AddNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
				}
			}

			// sleep for 5 seconds then check again
			time.Sleep(5 * time.Second)
		}
	}()
}

// Create new Client struct instance and sets up node ring with consistent hashing
func NewClientWrapper(config_file string) *ClientWrapper {
	// get initial nodes from config file and add them to the ring
	init_nodes_config := nodelib.LoadNodesConfig(config_file)
	ring := ring.NewRing()
	var cluster_config []*pb.Node

	for _, node := range init_nodes_config.Nodes {
		c, err := NewCacheClient(node.Host, int(node.GrpcPort))
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}
		node.SetGrpcClient(c)
		
		// try getting config from node
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		res, err := c.GetClusterConfig(ctx, &pb.ClusterConfigRequest{CallerNodeId: "client"})
		if err != nil {
			log.Printf("error getting cluster config form node %s: %v", node.Id, err)
			continue
		}
		cluster_config = res.Nodes
		break
	}

	log.Printf("initial cluster config: %v", cluster_config)

	// create config map from ring
	config_map := make(map[string]*nodelib.Node)
	for _, node := range cluster_config {
		// add to config map
		config_map[node.Id] = nodelib.NewNode(node.Id, node.Host, node.RestPort, node.GrpcPort)
		
		// add to ring
		ring.AddNode(node.Id, node.Host, node.RestPort, node.GrpcPort)

		// attempt to create client
		c, err := NewCacheClient(node.Host, int(node.GrpcPort))
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}
		config_map[node.Id].SetGrpcClient(c)
	}
	config := nodelib.NodesConfig{Nodes: config_map}
	return &ClientWrapper{Config: config, Ring: ring}
}

// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func NewCacheClient(server_host string, server_port int) (pb.CacheServiceClient, error) {
	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		log.Fatalf("failed to create credentials: %v", err)
	}

	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping back
		PermitWithoutStream: true,             // send pings even without active streams
	}

	// set up connection
	addr := fmt.Sprintf("%s:%d", server_host, server_port)
	conn, err := grpc.Dial(
		addr,	
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(kacp),
		grpc.WithTimeout(time.Duration(time.Second)),
	)
	if err != nil {
		return nil, errors.New("unable to connect to node")
	}	

	// set up client
	return pb.NewCacheServiceClient(conn), nil
}

// Get item from cache. Hash the key to find which node has the value stored.
func (c *ClientWrapper) Get(key string) string {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make request
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/get", nodeInfo.Host, nodeInfo.RestPort))

	if err != nil {
        log.Fatal(err)
    }

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)

    if err != nil {
        log.Fatal(err)
    }

    return string(body)
}

// Put key-value pair into cache. Hash the key to find which node to store it in.
func (c *ClientWrapper) Put(key string, value string) string {
	// find node to put the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// encode json payload
	payload := Payload{Key: key, Value: value}
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(payload)

	// make request
	host := fmt.Sprintf("http://%s:%d/put", nodeInfo.Host, nodeInfo.RestPort)
	req, err := http.NewRequest("POST", host, b)
	if err != nil {
	   log.Fatal(err)
	}

	// check response
	resp, err := new(http.Client).Do(req)
	if err != nil {
		log.Fatal(err)
	}

    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        log.Fatal(err)
    }
	return string(body)
}

func (c *ClientWrapper) GetGrpc(key string) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make new client if necessary
	if nodeInfo.GrpcClient == nil {
		c, err := NewCacheClient(nodeInfo.Host, int(nodeInfo.GrpcPort))
		if err != nil {
			log.Printf("error: %v", err)
			return
		}
		nodeInfo.SetGrpcClient(c)
	}

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	res, err := nodeInfo.GrpcClient.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		log.Printf("Error getting key '%s' from cache: %v", key, err)
		return
	}
	log.Printf(res.GetData())
}

func (c *ClientWrapper) PutGrpc(key string, value string) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make new client if necessary
	if nodeInfo.GrpcClient == nil {
		c, err := NewCacheClient(nodeInfo.Host, int(nodeInfo.GrpcPort))
		if err != nil {
			log.Printf("error: %v", err)
			return
		}
		nodeInfo.SetGrpcClient(c)
	}

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	_, err := nodeInfo.GrpcClient.Put(ctx, &pb.PutRequest{Key: key, Value: value})
	if err != nil {
		log.Printf("Error putting key '%s' value '%s' into cache: %v", key, value, err)
		for _, ringnode := range c.Ring.Nodes {
			log.Printf("ringnode %s", ringnode.Id)
		}
		log.Printf("key %s nearest node is %s", key, nodeId)
		return
	}
}

// Utility function to set up mTLS config and credentials
func LoadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile("../../certs/ca-cert.pem")
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair("../../certs/client-cert.pem", "../../certs/client-key.pem")
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      certPool,
	}

	return credentials.NewTLS(config), nil
}

