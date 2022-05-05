// This package defines an Identity CacheServer which uses gRPC secured with mTLS and a local Redis server
// as an in-memory data store which periodically persists to disk (and persists to disk on shutdown as well).
package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"

	"go.uber.org/zap"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	"github.com/malwaredllc/minicache/lru_cache"
	"sync"
)

// Identity server
type CacheServer struct {
	cache				*lru_cache.LruCache
	logger 				*zap.SugaredLogger 
	nodes_config 		node.NodesConfig
	leader_id 			int32
	node_id 			int32
	shutdown_chan 		chan bool
	decision_chan		chan int32
	sync_complete		chan bool	
	vector_clock 		*VectorClock
	mutex 				sync.RWMutex
	election_status 	bool
	pb.UnimplementedCacheServiceServer
}

// Utility function for creating a new gRPC server secured with mTLS, and registering a cache server service with it.
// Returns tuple of (gRPC server instance, registered Cache CacheServer instance).
func GetNewCacheServer(config_file string, verbose bool) (*grpc.Server, *CacheServer) {	
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

	// create server instance
	cache_server := CacheServer{
		logger: 			sugared_logger,
		nodes_config: 	 	nodes_config,
		node_id: 			node_id,
		leader_id: 			NO_LEADER,
		shutdown_chan:		make(chan bool, 1),
		decision_chan: 		make(chan int32, 1),
		sync_complete:		make(chan bool, 1),
		vector_clock: 		clock,
		election_status: 	NO_ELECTION_RUNNING,
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
		sync_complete:	make(chan bool),
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
