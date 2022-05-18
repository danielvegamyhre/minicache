// This package defines a LRU cache server
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	empty "github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/malwaredllc/minicache/lru_cache"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/pb"
	"go.uber.org/zap"

	"sync"

	"github.com/gin-gonic/gin"
)

const (
	PROD_DB = 0
	TEST_DB = 1
	SUCCESS = "OK"
)

type CacheServer struct {
	router          *gin.Engine
	cache           *lru_cache.LruCache
	logger          *zap.SugaredLogger
	nodes_config    node.NodesConfig
	leader_id       string
	node_id         string
	group_id        string
	client_auth     bool
	https_enabled   bool
	shutdown_chan   chan bool
	decision_chan   chan string
	mutex           sync.RWMutex
	election_status bool
	pb.UnimplementedCacheServiceServer
}

type Pair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ServerComponents struct {
	GrpcServer *grpc.Server
	HttpServer *http.Server
}

const (
	DYNAMIC = "DYNAMIC"
)

// Utility function for creating a new gRPC server secured with mTLS, and registering a cache server service with it.
// Set node_id param to DYNAMIC to dynamically discover node id.
// Otherwise, manually set it to a valid node_id from the config file.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func NewCacheServer(capacity int, config_file string, verbose bool, node_id string, https_enabled bool, client_auth bool) (*grpc.Server, *CacheServer) {
	// get nodes config
	nodes_config := node.LoadNodesConfig(config_file)

	// set up logging
	sugared_logger := GetSugaredZapLogger(
						nodes_config.ServerLogfile, 
						nodes_config.ServerErrfile, 
						verbose,
					  )

	// determine which node id we are and which group we are in
	var final_node_id string
	if node_id == DYNAMIC {
		log.Printf("passed node id: %s", node_id)
		final_node_id = node.GetCurrentNodeId(nodes_config)
		log.Printf("final node id: %s", final_node_id)

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
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// initialize LRU cache
	lru := lru_cache.NewLruCache(capacity)

	// create server instance
	cache_server := CacheServer{
		router:        router,
		cache:         &lru,
		logger:        sugared_logger,
		nodes_config:  nodes_config,
		node_id:       final_node_id,
		leader_id:     NO_LEADER,
		client_auth:   client_auth,
		https_enabled: https_enabled,
		decision_chan: make(chan string, 1),
	}

	cache_server.router.GET("/get/:key", cache_server.GetHandler)
	cache_server.router.POST("/put", cache_server.PutHandler)
	var grpc_server *grpc.Server
	if https_enabled {

		creds, err := LoadTLSCredentials(client_auth)
		if err != nil {
			cache_server.logger.Fatalf("failed to create credentials: %v", err)
		}

		// register service instance with gRPC server
		grpc_server = grpc.NewServer(grpc.Creds(creds))
	} else {
		grpc_server = grpc.NewServer()
	}

	// set up TLS
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

func (s *CacheServer) RunAndReturnHttpServer(port int) *http.Server {
	// setup http server
	addr := fmt.Sprintf(":%d", port)
	var tlsConfig *tls.Config
	if s.https_enabled {
		tlsConfig, _ = LoadTlsConfig(s.client_auth)
	}
	srv := &http.Server{
		Addr:      addr,
		Handler:   s.router,
		TLSConfig: tlsConfig,
	}

	// run in background
	go func() {
		// service connections
		if s.https_enabled {
			if err := srv.ListenAndServeTLS("certs/server-cert.pem", "certs/server-key.pem"); err != nil {
				log.Printf("listen: %s\n via HTTPS", err)
			}
		} else {
			if err := srv.ListenAndServe(); err != nil {
				log.Printf("listen: %s\n via HTTP", err)
			}
		}
	}()

	// return server object so we can shutdown gracefully later
	return srv
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


// Utility funciton to get a new Cache Client which uses gRPC secured with mTLS
func (s *CacheServer) NewCacheClient(server_host string, server_port int) (pb.CacheServiceClient, error) {

	var conn *grpc.ClientConn
	var err error

	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping back
		PermitWithoutStream: true,             // send pings even without active streams
	}

	// set up connection
	addr := fmt.Sprintf("%s:%d", server_host, server_port)
	if err != nil {
		return nil, err
	}

	if s.https_enabled {
		// set up TLS
		creds, err := LoadTLSCredentials(s.client_auth)
		if err != nil {
			s.logger.Fatalf("failed to create credentials: %v", err)
		}

		conn, err = grpc.Dial(
			addr,
			grpc.WithTransportCredentials(creds),
			grpc.WithKeepaliveParams(kacp),
			grpc.WithTimeout(time.Duration(time.Second)),
		)
	} else {

		conn, err = grpc.Dial(
			addr,
			grpc.WithInsecure(),
			grpc.WithKeepaliveParams(kacp),
			grpc.WithTimeout(time.Duration(time.Second)),
		)
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
			Id:       local_node.Id,
			Host:     local_node.Host,
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

// Log function that can be called externally
func (s *CacheServer) LogInfoLevel(msg string) {
	s.logger.Info(msg)
}

// Create and run all servers defined in config file and return list of server components
func CreateAndRunAllFromConfig(capacity int, config_file string, verbose bool, insecure_http bool) []ServerComponents {
	config := node.LoadNodesConfig(config_file)

	// set up logging
	logger := GetSugaredZapLogger(
						config.ServerLogfile, 
						config.ServerErrfile, 
						verbose,
					  )

	var components []ServerComponents

	for _, nodeInfo := range config.Nodes {
		// set up listener TCP connectiion
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", nodeInfo.GrpcPort))
		if err != nil {
			panic(err)
		}

		// get new grpc id server
		grpc_server, cache_server := NewCacheServer(
			capacity, 
			config_file, 
			verbose, 
			nodeInfo.Id, 
			config.EnableHttps, 
			config.EnableClientAuth,
		)

		// run gRPC server
		logger.Infof(fmt.Sprintf("Node %s running gRPC server on port %d...", nodeInfo.Id, nodeInfo.GrpcPort))
		go grpc_server.Serve(listener)

		// register node with cluster
		cache_server.RegisterNodeInternal()

		// run initial election
		cache_server.RunElection()

		// start leader heartbeat monitor
		go cache_server.StartLeaderHeartbeatMonitor()

		// run HTTP server
		logger.Infof(fmt.Sprintf("Node %s running REST API server on port %d...", nodeInfo.Id, nodeInfo.RestPort))
		http_server := cache_server.RunAndReturnHttpServer(int(nodeInfo.RestPort))

		components = append(components, ServerComponents{GrpcServer: grpc_server, HttpServer: http_server})
	}
	return components
}


//Set up mutual TLS config
func LoadTlsConfig(client_auth bool) (*tls.Config, error) {
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

	var clientAuth tls.ClientAuthType
	if client_auth {
		clientAuth = tls.RequireAndVerifyClientCert
	} else {
		clientAuth = tls.NoClientCert
	}

	// Create the credentials and return it
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   clientAuth,
		ClientCAs:    certPool,
		RootCAs:      certPool,
	}

	return config, nil

}

// Set up mutual TLS config and credentials
func LoadTLSCredentials(client_auth bool) (credentials.TransportCredentials, error) {
	config, err := LoadTlsConfig(client_auth)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(config), nil
}

// Set up logger at the specified verbosity level
func GetSugaredZapLogger(log_file string, err_file string, verbose bool) *zap.SugaredLogger {
	var level zap.AtomicLevel
	output_paths := []string{log_file}
	error_paths := []string{err_file}

	// also log to console in verbose mode
	if verbose {
		level = zap.NewAtomicLevelAt(zap.DebugLevel)
		output_paths = append(output_paths, "stdout")
	} else {
		level = zap.NewAtomicLevelAt(zap.ErrorLevel)
		error_paths = append(output_paths, "stderr")
	}
	cfg := zap.Config{
		Level:            level,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      output_paths,
		ErrorOutputPaths: error_paths,
	}
	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	return logger.Sugar()
}

// New gRPC client for a server node
func NewGrpcClientForNode(node *node.Node, client_auth bool, https_enabled bool) pb.CacheServiceClient {
	// set up TLS
	var conn *grpc.ClientConn
	var err error
	if https_enabled {
		creds, err := LoadTLSCredentials(client_auth)
		if err != nil {
			panic(fmt.Sprintf("failed to create credentials: %v", err))
		}

		// set up grpc connection
		conn, err = grpc.Dial(fmt.Sprintf("%s:%d", node.Host, node.GrpcPort), grpc.WithTransportCredentials(creds))
	} else {
		conn, err = grpc.Dial(fmt.Sprintf("%s:%d", node.Host, node.GrpcPort), grpc.WithInsecure())
	}
	if err != nil {
		panic(err)
	}

	// new identity service client
	return pb.NewCacheServiceClient(conn)
}
