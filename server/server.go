// This package defines a cache server with the following features:
// - REST API endpoints client get/put operations
// - inter-node communciation via gRPC secured with mTLS 
// - fault tolerance via single-leader asynchronous replication
// - leader election via Bully Algorithm
// - vector clocks used for resolving conflicts
// - eventual consistency guarantee
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"log"
	"time"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/keepalive"	
	empty "github.com/golang/protobuf/ptypes/empty"

	"go.uber.org/zap"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/lru_cache"

	"sync"

    "github.com/gin-gonic/gin"
)

const (
	PROD_DB = 0
	TEST_DB = 1
	SUCCESS = "OK"
)

type CacheServer struct {
	router				*gin.Engine
	cache				*lru_cache.LruCache
	logger 				*zap.SugaredLogger 
	nodes_config 		node.NodesConfig
	leader_id 			string
	node_id 			string
	group_id			string
	shutdown_chan 		chan bool
	decision_chan		chan string
	mutex 				sync.RWMutex
	election_status 	bool
	pb.UnimplementedCacheServiceServer
}

type Pair struct {
	Key 	string	`json:"key"`
	Value	string 	`json:"value"`	
}

const (
	DYNAMIC = "DYNAMIC"
)

// Utility function for creating a new gRPC server secured with mTLS, and registering a cache server service with it.
// Set node_id param to DYNAMIC to dynamically discover node id. 
// Otherwise, manually set it to a valid node_id from the config file.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func GetNewCacheServer(capacity int, config_file string, verbose bool, node_id string) (*grpc.Server, *CacheServer) {	
	// set up logging
	sugared_logger := GetSugaredZapLogger(verbose)

	// get nodes config
	nodes_config := node.LoadNodesConfig(config_file)

	// determine which node id we are and which group we are in
	var final_node_id string
	if node_id == DYNAMIC {
		final_node_id = node.GetCurrentNodeId(nodes_config)

		// if this is not one of the initial nodes in the config file, add it dynamically
		if _, ok := nodes_config.Nodes[final_node_id]; !ok {
			host, _ := os.Hostname()
			nodes_config.Nodes[final_node_id] = node.NewNode(final_node_id, host, 8080, 5005)
		}
	} else {
		final_node_id = node_id

		// if this is not one of the initial nodes in the config file, panic
		if _, ok := nodes_config.Nodes[final_node_id]; !ok {
			panic("given node ID not found in config file")
		}
	}


	// set up gin router
	router := gin.New()
	router.Use(gin.Recovery())

	// initialize LRU cache
	lru := lru_cache.NewLruCache(capacity)

	// create server instance
	cache_server := CacheServer{
		router:				router,
		cache:				&lru,	
		logger: 			sugared_logger,
		nodes_config: 	 	nodes_config,
		node_id: 			node_id,
		leader_id: 			NO_LEADER,
		decision_chan:		make(chan string, 1),
	}

	cache_server.router.GET("/get/:key", cache_server.GetHandler)
	cache_server.router.POST("/put", cache_server.PutHandler)

	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		cache_server.logger.Fatalf("failed to create credentials: %v", err)
	}

	// register service instance with gRPC server
	grpc_server := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterCacheServiceServer(grpc_server, &cache_server)
	reflection.Register(grpc_server)
	return grpc_server, &cache_server
}

// GET /get/:key
// REST API endpoint to get value for key from the LRU cache
func (s *CacheServer) GetHandler(c *gin.Context) {
	result := make(chan gin.H)
	go func(ctx *gin.Context) {
		value, err := s.cache.Get(c.Param("key"))
		if err != nil {
			result <- gin.H{"message": "key not found"}
		} else {
			result <- gin.H{"value": value}
		}
	}(c.Copy())
	c.IndentedJSON(http.StatusOK, <-result)
}

// POST /put (Body: {"key": "1", "value": "2"})
// REST API endpoint to put a new key-value pair in the LRU cache
func (s *CacheServer) PutHandler(c *gin.Context) {
	result := make(chan gin.H)
	go func(ctx *gin.Context) {
    	var newPair Pair
		if err := c.BindJSON(&newPair); err != nil {
			s.logger.Errorf("unable to deserialize key-value pair from json")
			return
		}
		s.cache.Put(newPair.Key, newPair.Value)
		result <- gin.H{"key": newPair.Key, "value": newPair.Value}
	}(c.Copy())
    c.IndentedJSON(http.StatusCreated, <-result)
}

func (s *CacheServer) RunHttpServer(port int) {
	s.router.Run(fmt.Sprintf(":%d", port))
}

// gRPC handler for getting item from cache. Any replica in the group can serve read requests.
func (s *CacheServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	value, err := s.cache.Get(req.Key)
	if err != nil {
		return &pb.GetResponse{Data: "key not found"}, nil
	}
	return &pb.GetResponse{Data: value}, nil
}

// gRPC handler for putting item in cache. 
func (s *CacheServer) Put(ctx context.Context, req *pb.PutRequest) (*empty.Empty, error) {
	s.cache.Put(req.Key, req.Value)
	return &empty.Empty{}, nil	
}

// Set up mutual TLS config and credentials
func LoadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load certificate of the CA who signed client's certificate
	pemClientCA, err := ioutil.ReadFile("certs/ca-cert.pem")
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemClientCA) {
		return nil, fmt.Errorf("failed to add client CA's certificate")
	}

	// Load server's certificate and private key
	serverCert, err := tls.LoadX509KeyPair("certs/server-cert.pem", "certs/server-key.pem")
	if err != nil {
		return nil, err
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		RootCAs:	  certPool,
	}

	return credentials.NewTLS(config), nil
}

// Set up logger at the specified verbosity level
func GetSugaredZapLogger(verbose bool) *zap.SugaredLogger {
	var level zap.AtomicLevel
	if verbose {
		level = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	}
	cfg := zap.Config{
		Level:            level,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return logger.Sugar()
}

// New gRPC client for a server node
func NewGrpcClientForNode(node *node.Node) pb.CacheServiceClient {
	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		panic(fmt.Sprintf("failed to create credentials: %v", err))
	}

	// set up grpc connection
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", node.Host, node.GrpcPort), grpc.WithTransportCredentials(creds))
	if err != nil {
		panic(err)
	}

	// new identity service client
	return pb.NewCacheServiceClient(conn)	
}

// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func (s *CacheServer) NewCacheClient(server_host string, server_port int) (pb.CacheServiceClient, error) {
	// set up TLS
	creds, err := LoadTLSCredentials()
	if err != nil {
		s.logger.Fatalf("failed to create credentials: %v", err)
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
		return nil, err	
	}	

	// set up client
	return pb.NewCacheServiceClient(conn), nil
}

// Register node with the cluster. This is a function to be called internally by server code (as 
// opposed to the gRPC handler to register node, which is what receives the RPC sent by this function).
func (s *CacheServer) RegisterNodeInternal() {
	s.logger.Infof("attempting to register %s with cluster", s.node_id)
	local_node, _ := s.nodes_config.Nodes[s.node_id]

	// try to register with each node until one returns a successful response
	for _, node := range s.nodes_config.Nodes {
		// skip self
		if node.Id == s.node_id {
			continue
		}
		req := pb.Node{
			Id: local_node.Id, 
			Host: local_node.Host, 
			RestPort: local_node.RestPort, 
			GrpcPort: local_node.GrpcPort,
		}


		c, err := s.NewCacheClient(node.Host, int(node.GrpcPort))
		if err != nil {
			s.logger.Errorf("unable to connect to node %s", node.Id)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_, err = c.RegisterNodeWithCluster(ctx, &req)
		if err != nil {
			s.logger.Infof("error registering node %s with cluster: %v", s.node_id, err)
			continue
		}

		s.logger.Infof("node %s is registered with cluster", s.node_id)
		return
	}
}

func CreateAndRunAllFromConfig(capacity int, config_file string, verbose bool) {
	log.Printf("Creating and running all nodes from config file: %s", config_file)
	config := node.LoadNodesConfig(config_file)

	for _, nodeInfo := range config.Nodes {
		// set up listener TCP connectiion
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", nodeInfo.GrpcPort))
		if err != nil {
			panic(err)
		}

		// get new grpc id server
		grpc_server, cache_server := GetNewCacheServer(capacity, config_file, verbose, nodeInfo.Id)

		// run gRPC server
		log.Printf("Running gRPC server on port %d...", nodeInfo.GrpcPort)
		go grpc_server.Serve(listener)

		// register node with cluster
		cache_server.RegisterNodeInternal()

		// run initial election
		cache_server.RunElection()

		// start leader heartbeat monitor
		go cache_server.StartLeaderHeartbeatMonitor()


		// run HTTP server
		log.Printf("Running REST API server on port %d...", nodeInfo.RestPort)
		go cache_server.RunHttpServer(int(nodeInfo.RestPort))
	}
}
