package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	nodelib "github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/ring"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

type Payload struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Client wrapper for interacting with Identity Server
type ClientWrapper struct {
	Config  nodelib.NodesConfig
	Ring    *ring.Ring
	Logger  *zap.SugaredLogger
	CertDir string
}

// Create new Client struct instance and sets up node ring with consistent hashing
func NewClientWrapper(certDir string, configFile string, insecure bool, verbose bool) *ClientWrapper {

	// get initial nodes from config file and add them to the ring
	initNodesConfig := nodelib.LoadNodesConfig(configFile)

	// set up logging
	logger := GetSugaredZapLogger(
		initNodesConfig.ClientLogfile,
		initNodesConfig.ClientErrfile,
		verbose,
	)

	// set up consistent hashing ring and create grpc clients to active nodes
	ring := ring.NewRing()
	var clusterConfig []*pb.Node

	for _, node := range initNodesConfig.Nodes {
		c, err := NewCacheClient(certDir, node.Host, int(node.GrpcPort), initNodesConfig.EnableHttps)
		if err != nil {
			logger.Infof("error: %v", err)
			continue
		}
		node.SetGrpcClient(c)

		// try getting config from node
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		res, err := c.GetClusterConfig(ctx, &pb.ClusterConfigRequest{CallerNodeId: "client"})
		if err != nil {
			logger.Infof("error getting cluster config from node %s: %v", node.Id, err)
			continue
		}
		clusterConfig = res.Nodes
		break
	}

	logger.Infof("Client received initial cluster config: %v", clusterConfig)

	// create config map from ring
	configMap := make(map[string]*nodelib.Node)
	for _, node := range clusterConfig {
		// add to config map
		configMap[node.Id] = nodelib.NewNode(node.Id, node.Host, node.RestPort, node.GrpcPort)

		// add to ring
		ring.AddNode(node.Id, node.Host, node.RestPort, node.GrpcPort)

		// attempt to create client
		c, err := NewCacheClient(certDir, node.Host, int(node.GrpcPort), initNodesConfig.EnableHttps)
		if err != nil {
			logger.Infof("error: %v", err)
			continue
		}
		configMap[node.Id].SetGrpcClient(c)
	}
	config := nodelib.NodesConfig{
		Nodes:            configMap,
		EnableHttps:      initNodesConfig.EnableHttps,
		EnableClientAuth: initNodesConfig.EnableClientAuth,
	}

	return &ClientWrapper{Config: config, Ring: ring, CertDir: certDir, Logger: logger}
}

// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func NewCacheClient(cert_dir string, server_host string, server_port int, enableHTTPS bool) (pb.CacheServiceClient, error) {

	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping back
		PermitWithoutStream: true,             // send pings even without active streams
	}

	tlsConnectionOption := grpc.WithInsecure()

	if enableHTTPS {
		// set up TLS
		creds, err := LoadTLSCredentials(cert_dir)
		if err != nil {
			panic(fmt.Sprintf("failed to load TLS credentials: %v", err))
		}
		tlsConnectionOption = grpc.WithTransportCredentials(creds)
	}

	// set up connection
	addr := fmt.Sprintf("%s:%d", server_host, server_port)
	conn, err := grpc.Dial(
		addr,
		tlsConnectionOption,
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
func (c *ClientWrapper) Get(key string) (string, error) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make request
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/get", nodeInfo.Host, nodeInfo.RestPort))

	if err != nil {
		return "", errors.New(fmt.Sprintf("error sending GET request: %s", err))
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", errors.New(fmt.Sprintf("error reading response: %s", err))
	}

	return string(body), nil
}

// Put key-value pair into cache. Hash the key to find which node to store it in.
func (c *ClientWrapper) Put(key string, value string) error {
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
		return errors.New(fmt.Sprintf("error creating POST request: %s", err))
	}

	// check response
	res, err := new(http.Client).Do(req)

	if err != nil {
		return errors.New(fmt.Sprintf("error sending POST request: %s", err))
	}

	defer res.Body.Close()

	return nil
}

func (c *ClientWrapper) GetGrpc(key string) (string, error) {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make new client if necessary
	if nodeInfo.GrpcClient == nil {
		c, err := NewCacheClient(c.CertDir, nodeInfo.Host, int(nodeInfo.GrpcPort), c.Config.EnableHttps)
		if err != nil {
			return "", errors.New(fmt.Sprintf("error making gRPC client: %s", err))
		}
		nodeInfo.SetGrpcClient(c)
	}

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	res, err := nodeInfo.GrpcClient.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		return "", errors.New(fmt.Sprintf("error making gRPC Get call: %s", err))
	}
	return res.GetData(), nil
}

func (c *ClientWrapper) PutGrpc(key string, value string) error {
	// find node which contains the item
	nodeId := c.Ring.Get(key)
	nodeInfo := c.Config.Nodes[nodeId]

	// make new client if necessary
	if nodeInfo.GrpcClient == nil {
		c, err := NewCacheClient(c.CertDir, nodeInfo.Host, int(nodeInfo.GrpcPort), c.Config.EnableHttps)
		if err != nil {
			return errors.New(fmt.Sprintf("error making gRPC client: %s", err))
		}
		nodeInfo.SetGrpcClient(c)
	}

	// create context
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make request
	_, err := nodeInfo.GrpcClient.Put(ctx, &pb.PutRequest{Key: key, Value: value})
	if err != nil {
		return errors.New(fmt.Sprintf("error making gRPC Put call: %s", err))
	}
	return nil
}

// Utility function to set up mTLS config and credentials
func LoadTLSCredentials(certDir string) (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile(fmt.Sprintf("%s/ca-cert.pem", certDir))
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Load client's certificate and private key
	clientCert, err := tls.LoadX509KeyPair(
		fmt.Sprintf("%s/client-cert.pem", certDir),
		fmt.Sprintf("%s/client-key.pem", certDir),
	)
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

// Set up logger at the specified verbosity level
func GetSugaredZapLogger(logFile string, errFile string, verbose bool) *zap.SugaredLogger {
	var level zap.AtomicLevel
	outputPaths := []string{logFile}
	errorPaths := []string{errFile}

	// also log to console in verbose mode
	if verbose {
		level = zap.NewAtomicLevelAt(zap.DebugLevel)
		outputPaths = append(outputPaths, "stdout")
	} else {
		level = zap.NewAtomicLevelAt(zap.ErrorLevel)
		errorPaths = append(outputPaths, "stderr")
	}
	cfg := zap.Config{
		Level:            level,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      outputPaths,
		ErrorOutputPaths: errorPaths,
	}
	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return logger.Sugar()
}

// Checks cluster config every 5 seconds and updates ring with any changes. Runs in infinite loop.
func (c *ClientWrapper) StartClusterConfigWatcher(shutdownChan <-chan bool) {
	go func() {
		// get cluster config every 1 second until shutdown signal received
		for {
			select {
			case <-shutdownChan:
				return
			case <-time.After(time.Second):
				c.fetchClusterConfig()
			}
		}
	}()
}

func (c *ClientWrapper) fetchClusterConfig() {
	// ask random nodes who the leader until we get a response.
	// we want each client to ask random nodes, not fixed, to avoid overloading one with leader requests
	var leader *nodelib.Node
	attempted := make(map[string]bool)
	for {
		randnode := nodelib.GetRandomNode(c.Ring.Nodes)

		// if all nodes are unavailable, throw error
		if len(attempted) == len(c.Ring.Nodes) {
			c.Logger.Fatalf("error: unable to connect to any nodes")
		}

		// skip attempted nodes
		if _, ok := attempted[randnode.Id]; ok {
			c.Logger.Infof("Skipping visited node %s...", randnode.Id)
			continue
		}

		// mark visited and request leader
		attempted[randnode.Id] = true

		// skip node we can't connect to
		client, err := NewCacheClient(c.CertDir, randnode.Host, int(randnode.GrpcPort), c.Config.EnableHttps)
		if err != nil {
			c.Logger.Infof("NewCacheClient failed %s...", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		res, err := client.GetLeader(ctx, &pb.LeaderRequest{Caller: "client"})
		if err != nil {
			c.Logger.Infof("GetLeader failed %s...", err)
			continue
		}

		leader = c.Config.Nodes[res.Id]
		break
	}

	if leader == nil {
		return
	}

	// get cluster config from current leader
	req := pb.ClusterConfigRequest{CallerNodeId: "client"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// restart process if we can't connect to leader
	client, err := NewCacheClient(c.CertDir, leader.Host, int(leader.GrpcPort), c.Config.EnableHttps)
	if err != nil {
		return
	}

	res, err := client.GetClusterConfig(ctx, &req)
	if err != nil {
		c.Logger.Infof("error getting cluster config from node %s: %v", leader.Id, err)
		return
	}

	// create hashmap of online node IDs to find missing node in constant time
	clusterNodes := make(map[string]bool)
	for _, nodecfg := range res.Nodes {
		clusterNodes[nodecfg.Id] = true
	}

	// remove missing nodes from ring
	for _, node := range c.Config.Nodes {
		if _, ok := clusterNodes[node.Id]; !ok {
			c.Logger.Infof("Removing node %s from ring", node.Id)
			delete(c.Config.Nodes, node.Id)
			c.Ring.RemoveNode(node.Id)
		}
	}

	// add new nodes to ring
	for _, nodecfg := range res.Nodes {
		if _, ok := c.Config.Nodes[nodecfg.Id]; !ok {
			c.Logger.Infof("Adding node %s to ring", nodecfg.Id)
			c.Config.Nodes[nodecfg.Id] = nodelib.NewNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
			c.Ring.AddNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
		}
	}
}
