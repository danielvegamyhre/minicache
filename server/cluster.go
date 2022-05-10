package server

import (
	"context"
	"github.com/malwaredllc/minicache/pb"
	"github.com/malwaredllc/minicache/node"
	empty "github.com/golang/protobuf/ptypes/empty"
	"time"
)

// gRPC handler for registering a new node with the cluster
func (s *CacheServer) RegisterNodeWithCluster(ctx context.Context, nodeInfo *pb.Node) (*pb.GenericResponse, error) {
	// add node to ring
	s.Ring.AddNode(nodeInfo.Id, nodeInfo.Host, nodeInfo.RestPort, nodeInfo.GrpcPort)
	s.logger.Infof("Added node %s to ring", nodeInfo.Id)
	
	// add node to hashmap config for easy lookup
	s.nodes_config.Nodes[nodeInfo.Id] = node.NewNode(nodeInfo.Id, nodeInfo.Host, nodeInfo.RestPort, nodeInfo.GrpcPort)

	// send update to other nodes in cluster
	var nodes []*pb.Node
	for _, ringnode := range s.Ring.Nodes {
		nodes = append(nodes, &pb.Node{Id: ringnode.Id, Host: ringnode.Host, RestPort: ringnode.RestPort, GrpcPort: ringnode.GrpcPort})
	}
	for _, ringnode := range s.Ring.Nodes {
		// create context
		req_ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		cfg := pb.ClusterConfig{Nodes: nodes}

		ringnode.GrpcClient.UpdateClusterConfig(req_ctx, &cfg)
	}
	return &pb.GenericResponse{Data: SUCCESS}, nil
}

// gRPC handler for getting cluster config
func (s *CacheServer) GetClusterConfig(ctx context.Context, req *pb.ClusterConfigRequest) (*pb.ClusterConfig, error) {
	s.logger.Infof("Returning cluster config to node %s", req.CallerNodeId)
	var nodes []*pb.Node
	for _, ringnode := range s.Ring.Nodes {
		nodes = append(nodes, &pb.Node{Id: ringnode.Id, Host: ringnode.Host, RestPort: ringnode.RestPort, GrpcPort: ringnode.GrpcPort})
	}
	return &pb.ClusterConfig{Nodes: nodes}, nil
}

// gRPC handler for updating cluster config
func (s *CacheServer) UpdateClusterConfig(ctx context.Context, req *pb.ClusterConfig) (*empty.Empty, error) {
	s.logger.Info("Updating cluster config")
	// for each node in incoming config, if it isn't in our current config, add it
	for _, nodecfg := range req.Nodes {
		if _, ok := s.nodes_config.Nodes[nodecfg.Id]; !ok {
			s.Ring.AddNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
			s.nodes_config.Nodes[nodecfg.Id] = node.NewNode(nodecfg.Id, nodecfg.Host, nodecfg.RestPort, nodecfg.GrpcPort)
			s.logger.Infof("Added new node %s to ring", nodecfg.Id)
		}
	}
	return &empty.Empty{}, nil
}