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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/keepalive"	
	empty "github.com/golang/protobuf/ptypes/empty"

	"go.uber.org/zap"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/lru_cache"
	"github.com/malwaredllc/minicache/ring"

	"sync"

    "github.com/gin-gonic/gin"
)

const (
	PROD_DB = 0
	TEST_DB = 1
	SUCCESS = "OK"
)

type CacheServer struct {
	Ring				ring.Ring
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

// Utility function for creating a new gRPC server secured with mTLS, and registering a cache server service with it.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func GetNewCacheServer(capacity int, config_file string, verbose bool) (*grpc.Server, *CacheServer) {	
	// set up logging
	sugared_logger := GetSugaredZapLogger(verbose)

	// get nodes config
	nodes_config := node.LoadNodesConfig(config_file)

	// determine which node id we are and which group we are in
	node_id := node.GetCurrentNodeId(nodes_config)

	// if this is not one of the initial nodes in the config file, add it dynamically
	if _, ok := nodes_config.Nodes[node_id]; !ok {
		host, _ := os.Hostname()
		nodes_config.Nodes[node_id] = node.NewNode(node_id, host, 8080, 5005)
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
func (s *CacheServer) NewCacheClient(server_host string, server_port int) pb.CacheServiceClient {
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
		panic(err)
	}	

	// set up client
	return pb.NewCacheServiceClient(conn)
}

// Register node with the cluster. This is a function to be called internally by server code (as 
// opposed to the gRPC handler to register node, which is what receives the RPC sent by this function).
func (s *CacheServer) RegisterNodeInternal() {
	// wait for leader to be elected if not done
	for {
		if s.leader_id == NO_LEADER {
			time.Sleep(time.Second)
			continue
		}
		break
	}

	leader, _ := s.nodes_config.Nodes[s.leader_id]
	local_node, _ := s.nodes_config.Nodes[s.node_id]
	req := pb.Node{
		Id: local_node.Id, 
		Host: local_node.Host, 
		RestPort: local_node.RestPort, 
		GrpcPort: local_node.GrpcPort,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if leader.GrpcClient == nil {
		c := s.NewCacheClient(leader.Host, int(leader.GrpcPort))
		leader.SetGrpcClient(c)
	}

	_, err := leader.GrpcClient.RegisterNodeWithCluster(ctx, &req)
	if err != nil {
		s.logger.Infof("error registering node %s with cluster: %v", s.node_id, err)
		return
	}
	s.logger.Infof("registered node %s with cluster", s.node_id)
}

// Set up a gRPC client for each node in cluster
func (s *CacheServer) CreateAllGrpcClients() {
	for _, node := range s.nodes_config.Nodes {
		c := s.NewCacheClient(node.Host, int(node.GrpcPort))
		node.SetGrpcClient(c)
		s.logger.Infof("Created gRPC client to %s", node.Id)
	}
}
    
