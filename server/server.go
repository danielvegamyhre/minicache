// This package defines a LRU cache server which supports client-side consistent hashing, 
// TLS (and mTLS), client access via both HTTP/gRPC, 
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
	nodesConfig    node.NodesConfig
	leaderID       string
	nodeID         string
	groupID        string
	clientAuth     bool
	httpsEnabled   bool
	shutdownChan   chan bool
	decisionChan   chan string
	mutex           sync.RWMutex
	electionStatus bool
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
// Otherwise, manually set it to a valid nodeID from the config file.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func NewCacheServer(capacity int, configFile string, verbose bool, nodeID string, httpsEnabled bool, clientAuth bool) (*grpc.Server, *CacheServer) {
	// get nodes config
	nodesConfig := node.LoadNodesConfig(configFile)

	// set up logging
	sugared_logger := GetSugaredZapLogger(
						nodesConfig.ServerLogfile, 
						nodesConfig.ServerErrfile, 
						verbose,
					  )

	// determine which node id we are and which group we are in
	var finalNodeID string
	if nodeID == DYNAMIC {
		log.Printf("passed node id: %s", nodeID)
		finalNodeID = node.GetCurrentNodeId(nodesConfig)
		log.Printf("final node id: %s", finalNodeID)

		// if this is not one of the initial nodes in the config file, add it dynamically
		if _, ok := nodesConfig.Nodes[finalNodeID]; !ok {
			host, _ := os.Hostname()
			nodesConfig.Nodes[finalNodeID] = node.NewNode(finalNodeID, host, 8080, 5005)
		}
	} else {
		finalNodeID = nodeID

		// if this is not one of the initial nodes in the config file, panic
		if _, ok := nodesConfig.Nodes[finalNodeID]; !ok {
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
	cacheServer := CacheServer{
		router:        router,
		cache:         &lru,
		logger:        sugared_logger,
		nodesConfig:  nodesConfig,
		nodeID:       finalNodeID,
		leaderID:     NO_LEADER,
		clientAuth:   clientAuth,
		httpsEnabled: httpsEnabled,
		decisionChan: make(chan string, 1),
	}

	cacheServer.router.GET("/get/:key", cacheServer.GetHandler)
	cacheServer.router.POST("/put", cacheServer.PutHandler)
	var grpcServer *grpc.Server
	if httpsEnabled {

		creds, err := LoadTLSCredentials(clientAuth)
		if err != nil {
			cacheServer.logger.Fatalf("failed to create credentials: %v", err)
		}

		// register service instance with gRPC server
		grpcServer = grpc.NewServer(grpc.Creds(creds))
	} else {
		grpcServer = grpc.NewServer()
	}

	// set up TLS
	pb.RegisterCacheServiceServer(grpcServer, &cacheServer)
	reflection.Register(grpcServer)
	return grpcServer, &cacheServer
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

func (s *CacheServer) RunAndReturnHTTPServer(port int) *http.Server {
	// setup http server
	addr := fmt.Sprintf(":%d", port)
	var tlsConfig *tls.Config
	if s.httpsEnabled {
		tlsConfig, _ = LoadTlsConfig(s.clientAuth)
	}
	srv := &http.Server{
		Addr:      addr,
		Handler:   s.router,
		TLSConfig: tlsConfig,
	}

	// run in background
	go func() {
		// service connections
		if s.httpsEnabled {
			if err := srv.ListenAndServeTLS("certs/server-cert.pem", "certs/server-key.pem"); err != nil {
				s.logger.Infof("listen: %s\n via HTTPS", err)
			}
		} else {
			if err := srv.ListenAndServe(); err != nil {
				s.logger.Infof("listen: %s\n via HTTP", err)
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


// Utility function to get a new Cache Client which uses gRPC secured with mTLS
func (s *CacheServer) NewCacheClient(serverHost string, serverPort int) (pb.CacheServiceClient, error) {

	var conn *grpc.ClientConn
	var err error

	var kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping back
		PermitWithoutStream: true,             // send pings even without active streams
	}

	// set up connection
	addr := fmt.Sprintf("%s:%d", serverHost, serverPort)
	if err != nil {
		return nil, err
	}

	if s.httpsEnabled {
		// set up TLS
		creds, err := LoadTLSCredentials(s.clientAuth)
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
	s.logger.Infof("attempting to register %s with cluster", s.nodeID)
	localNode, _ := s.nodesConfig.Nodes[s.nodeID]

	// try to register with each node until one returns a successful response
	for _, node := range s.nodesConfig.Nodes {
		// skip self
		if node.Id == s.nodeID {
			continue
		}
		req := pb.Node{
			Id:       localNode.Id,
			Host:     localNode.Host,
			RestPort: localNode.RestPort,
			GrpcPort: localNode.GrpcPort,
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
			s.logger.Infof("error registering node %s with cluster: %v", s.nodeID, err)
			continue
		}

		s.logger.Infof("node %s is registered with cluster", s.nodeID)
		return
	}
}

// Log function that can be called externally
func (s *CacheServer) LogInfoLevel(msg string) {
	s.logger.Info(msg)
}

// Create and run all servers defined in config file and return list of server components
func CreateAndRunAllFromConfig(capacity int, configFile string, verbose bool, insecureHTTP bool) []ServerComponents {
	config := node.LoadNodesConfig(configFile)

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
		grpcServer, cacheServer := NewCacheServer(
			capacity, 
			configFile, 
			verbose, 
			nodeInfo.Id, 
			config.EnableHttps, 
			config.EnableClientAuth,
		)

		// run gRPC server
		logger.Infof(fmt.Sprintf("Node %s running gRPC server on port %d...", nodeInfo.Id, nodeInfo.GrpcPort))
		go grpcServer.Serve(listener)

		// register node with cluster
		cacheServer.RegisterNodeInternal()

		// run initial election
		cacheServer.RunElection()

		// start leader heartbeat monitor
		go cacheServer.StartLeaderHeartbeatMonitor()

		// run HTTP server
		logger.Infof(fmt.Sprintf("Node %s running REST API server on port %d...", nodeInfo.Id, nodeInfo.RestPort))
		httpServer := cacheServer.RunAndReturnHTTPServer(int(nodeInfo.RestPort))

		components = append(components, ServerComponents{GrpcServer: grpcServer, HttpServer: httpServer})
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

// New gRPC client for a server node
func NewGrpcClientForNode(node *node.Node, clientAuth bool, httpsEnabled bool) pb.CacheServiceClient {
	// set up TLS
	var conn *grpc.ClientConn
	var err error
	if httpsEnabled {
		creds, err := LoadTLSCredentials(clientAuth)
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
