// This package defines a cache server with the following features:
// - REST API endpoints client get/put operations
// - inter-node communciation via gRPC secured with mTLS 
// - fault tolerance via single-leader asynchronous replication
// - leader election via Bully Algorithm
// - vector clocks used for resolving conflicts
// - eventual consistency guarantee
package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"go.uber.org/zap"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/lru_cache"
	"sync"
	"strconv"

    "github.com/gin-gonic/gin"
)

type CacheServer struct {
	router				*gin.Engine
	cache				*lru_cache.LruCache
	logger 				*zap.SugaredLogger 
	nodes_config 		node.NodesConfig
	leader_id 			int32
	node_id 			int32
	shutdown_chan 		chan bool
	decision_chan		chan int32
	vector_clock 		*VectorClock
	mutex 				sync.RWMutex
	election_status 	bool
	pb.UnimplementedCacheServiceServer
}

type Pair struct {
	Key 	string		`json:"key"`
	Value	string `json:"value"`	
}

// Utility function for creating a new gRPC server secured with mTLS, and registering a cache server service with it.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func GetNewCacheServer(capacity int, config_file string, verbose bool) (*grpc.Server, *CacheServer) {	
	// set up logging
	sugared_logger := GetSugaredZapLogger(verbose)

	// get nodes config
	nodes_config := node.LoadNodesConfig(config_file)

	// determine which node id we are
	node_id := node.GetCurrentNodeId(nodes_config)

	// restore local vector clock from disk if it exists.
	// this is so if all nodes go offline, when they reboot
	// they can check who processed the most recent changes
	clock := &VectorClock{NodeId: node_id, Logger: sugared_logger}
	clock.InitVector(len(nodes_config.Nodes))

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
		shutdown_chan:		make(chan bool, 1),
		decision_chan: 		make(chan int32, 1),
		vector_clock: 		clock,
		election_status: 	NO_ELECTION_RUNNING,
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
		key, err := strconv.Atoi(c.Param("key"))
		if err != nil {
			s.logger.Errorf("error getting key from request context: %v", err)
			return
		}
		value, err := s.cache.Get(key)
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
		key, _ := strconv.Atoi(newPair.Key)
		value, _ := strconv.Atoi(newPair.Value)
		s.cache.Put(key, value)
		result <- gin.H{"key": key, "value": value}
	}(c.Copy())
    c.IndentedJSON(http.StatusCreated, <-result)
}

func (s *CacheServer) RunHttpServer(port int) {
	s.router.Run(fmt.Sprintf(":%d", port))
}

// Utility function for creating a new gRPC server secured with mTLS in test mode.
// This runs the server in single-node mode.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func GetNewTestTestCacheServer(verbose bool) (*grpc.Server, *CacheServer) {	
	// set up logging
	sugared_logger := GetSugaredZapLogger(verbose)

	// set node id
	node_id := int32(0)

	// restore local vector clock from disk if it exists.
	// this is so if all nodes go offline, when they reboot
	// they can check who processed the most recent changes
	clock := &VectorClock{NodeId: node_id, Logger: sugared_logger}
	clock.InitVector(1) 

	// create server instance
	cache_server := CacheServer{
		logger: 		sugared_logger,
		nodes_config:  	node.NodesConfig{},
		node_id: 		node_id,
		leader_id: 		NO_LEADER,
		shutdown_chan:	make(chan bool),
		decision_chan: 	make(chan int32),
		vector_clock: 	clock,
	}

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
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", node.Host, node.Port), grpc.WithTransportCredentials(creds))
	if err != nil {
		panic(err)
	}

	// new identity service client
	return pb.NewCacheServiceClient(conn)	
}
