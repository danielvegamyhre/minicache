package server

import (
	"fmt"
	"context"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	empty "github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"
	"time"
)

// gRPC handler for registering a new node with the cluster. 
// New nodes call this RPC on the leader when they come online.
func (s *CacheServer) RegisterNodeWithCluster(ctx context.Context, nodeInfo *pb.Node) (*pb.GenericResponse, error) {	
	// if we already have this node registered, return
	if _, ok := s.nodes_config.Nodes[nodeInfo.Id]; ok {
		s.logger.Infof("Node %s already part of cluster", nodeInfo.Id)
		return &pb.GenericResponse{Data: SUCCESS}, nil
	}

	// add node to hashmap config for easy lookup
	s.nodes_config.Nodes[nodeInfo.Id] = node.NewNode(nodeInfo.Id, nodeInfo.Host, nodeInfo.RestPort, nodeInfo.GrpcPort)

	// setup grpc client for new node
	c, err := s.NewCacheClient(nodeInfo.Host, int(nodeInfo.GrpcPort))
	if err != nil {
		s.logger.Errorf("unable to connect to node %s", nodeInfo.Id)
		return nil, status.Errorf(
            codes.InvalidArgument,
            fmt.Sprintf("Unable to connect to node being registered: %s", nodeInfo.Id),
        )
	}
	s.nodes_config.Nodes[nodeInfo.Id].SetGrpcClient(c)
	s.logger.Infof("Added gprc client %v to node %s", c, nodeInfo.Id)

	// send update to other nodes in cluster
	var nodes []*pb.Node
	for _, node := range s.nodes_config.Nodes {
		nodes = append(nodes, &pb.Node{Id: node.Id, Host: node.Host, RestPort: node.RestPort, GrpcPort: node.GrpcPort})
	}
	for _, node := range s.nodes_config.Nodes {
		// skip self
		if node.Id == s.node_id {
			continue
		}
		s.logger.Infof("Sending updated cluster config to node %s with grpc client %v", node.Id, node.GrpcClient)
		// create context
		req_ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		cfg := pb.ClusterConfig{Nodes: nodes}

		node.GrpcClient.UpdateClusterConfig(req_ctx, &cfg)
	}
	return &pb.GenericResponse{Data: SUCCESS}, nil
}

// gRPC handler for getting cluster config
func (s *CacheServer) GetClusterConfig(ctx context.Context, req *pb.ClusterConfigRequest) (*pb.ClusterConfig, error) {
	var nodes []*pb.Node
	for _, node := range s.nodes_config.Nodes {
		nodes = append(nodes, &pb.Node{Id: node.Id, Host: node.Host, RestPort: node.RestPort, GrpcPort: node.GrpcPort})
	}
	s.logger.Infof("Returning cluster config to node %s: %v", req.CallerNodeId, nodes)
	return &pb.ClusterConfig{Nodes: nodes}, nil
}

// gRPC handler for updating cluster config with incoming info
func (s *CacheServer) UpdateClusterConfig(ctx context.Context, req *pb.ClusterConfig) (*empty.Empty, error) {
	s.logger.Info("Updating cluster config")
	// for each node in incoming config, if it isn't in our current config, add it
	s.nodes_config.Nodes = make(map[string]*node.Node)
	for _, nodecfg := range req.Nodes {

		// add new node to ring
		newNode := node.NewNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)

		// set up grpc client to new node
		c, err := s.NewCacheClient(nodecfg.Host, int(nodecfg.GrpcPort))
		if err != nil {
			s.logger.Errorf("unable to connect to node %s", nodecfg.Id)
			continue
		}
		newNode.SetGrpcClient(c)
		s.logger.Infof("added grpc client %v to node %s", c, nodecfg.Id)

		// add new node to config
		s.nodes_config.Nodes[nodecfg.Id] = newNode

	}
	return &empty.Empty{}, nil
}

// private function for server to send out updated cluster config to other nodes
func (s *CacheServer) updateClusterConfigInternal() {
	s.logger.Info("Sending out updated cluster config")

	// send update to other nodes in cluster
	var nodes []*pb.Node
	for _, node := range s.nodes_config.Nodes {
		nodes = append(nodes, &pb.Node{Id: node.Id, Host: node.Host, RestPort: node.RestPort, GrpcPort: node.GrpcPort})
	}
	for _, node := range s.nodes_config.Nodes {
		// skip self
		if node.Id == s.node_id {
			continue
		}
		// create context
		req_ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		cfg := pb.ClusterConfig{Nodes: nodes}

		_, err := node.GrpcClient.UpdateClusterConfig(req_ctx, &cfg)
		if err != nil {
			s.logger.Infof("error sending cluster config to node %s: %v", node.Id, err)
		}
	}
}