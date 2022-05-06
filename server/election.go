
package server

import (
	"context"
	"os"
	"time"
	"github.com/malwaredllc/minicache/pb"
)

// Run an election using the Bully Algorithm (https://en.wikipedia.org/wiki/Bully_algorithm)
func (s *CacheServer) RunElection() {
	// an individual node should run a single election process, not multiple concurrent ones
	if s.election_status == ELECTION_RUNNING {
		s.logger.Info("Election already running, waiting for completion...")
		return
	}

	// update status to election running
	s.election_status = ELECTION_RUNNING

	// check status of every node
	local_pid := int32(os.Getpid())
	s.logger.Infof("Running election. Local PID: %d", local_pid)

	for _, node := range s.nodes_config.Nodes {
		// skip self
		if node.Id == s.node_id {
			continue
		}

		// new identity service client
		client := NewGrpcClientForNode(node)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// make status request rpc
		res, err := client.GetPid(ctx, &pb.PidRequest{CallerPid: local_pid})
		if err != nil {
			s.logger.Infof("PID request to node %d failed", node.Id)
			continue
		}
		
		// if response has a higher PID (use node id as tie-breaker), we send it an election request and wait to receive the election winner announcement.
		s.logger.Infof("Received PID %d from node %d (vs local PID %d on node %d)", res.Pid, node.Id, local_pid, s.node_id)
		if (local_pid < res.Pid) || (res.Pid == local_pid && s.node_id < node.Id) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			
			s.logger.Infof("Sending election request to node %d", node.Id)
			_, err = client.RequestElection(ctx, &pb.ElectionRequest{CallerPid: local_pid, CallerNodeId: s.node_id})
			if err != nil {
				s.logger.Infof("Error requesting node %d run an election: %v", node.Id, err)
			}

			s.logger.Info("Waiting for decision...")
			// if after 5 seconds we receive no winner announcement, start the election process over
			select {
			case s.leader_id = <-s.decision_chan:
				s.logger.Infof("Received decision: Leader is node %d", s.leader_id)
				s.election_status = NO_ELECTION_RUNNING
				return	
			case <-time.After(5*time.Second):
				s.logger.Info("Timed out waiting for decision. Starting new election.")
				s.RunElection()
				s.election_status = NO_ELECTION_RUNNING
				return 
			}
		}
	}
	// if no other nodes have a higher PID, we are the winner
	s.leader_id = s.node_id

	// announce ourselves as winner to other nodes
	s.AnnounceNewLeader(s.leader_id)

	// reset election status
	s.election_status = NO_ELECTION_RUNNING
}

// Announce new leader to all nodes
func (s *CacheServer) AnnounceNewLeader(winner string) {
	s.logger.Infof("Announcing node %d won election",  winner)

	// if no response from any higher node IDs, declare self the winner and announce to all
	for _, node := range s.nodes_config.Nodes {
		// skip self
		if node.Id == s.node_id {
			continue
		}

		// new identity service client
		client := NewGrpcClientForNode(node)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// make status request rpc
		_, err := client.UpdateLeader(ctx, &pb.NewLeaderAnnouncement{LeaderId: winner})
		if err != nil {
			s.logger.Infof("Election winner announcement to node %d error: %v", node.Id, err)
			continue
		}
	}
}

// Checks if leader is alive every 1 second. If no response for 3 seconds, new election is held.
func (s *CacheServer) StartLeaderHeartbeatMonitor() {
	// wait for decision to get leader
	s.logger.Info("Leader heartbeat monitor starting...")

    ticker := time.NewTicker(time.Second)
    for {
			// run heartbeat check every 1 second
            <-ticker.C

			// if leader isn't alive, run new election
			if !s.IsLeaderAlive() {
				s.logger.Info("Leader heartbeat failed, running new election")
				s.RunElection()
				s.logger.Info("Election done, leader heartbeat continuing")
			}

			select {
			case <-s.shutdown_chan:
				s.logger.Info("Received shutdown signal")
				break
			case <-time.After(time.Second):
				continue
			}
        }
}

// Check if leader node is alive (3 second timeout)
func (s *CacheServer) IsLeaderAlive() bool {
	// make sure leader exists
	if s.leader_id == NO_LEADER {
		s.logger.Infof("IsLeaderAlive found leader doesn't exist")
		return false
	}
	// if this node is the leader, return true
	if s.node_id == s.leader_id {
		return true
	}
	leader := s.nodes_config.Nodes[s.leader_id]

	// new identity service client
	client := NewGrpcClientForNode(leader)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make status request rpc
	_, err := client.GetHeartbeat(ctx, &pb.HeartbeatRequest{CallerNodeId: s.node_id})
	if err != nil {
		s.logger.Infof("Leader healthcheck returned error: %v", err)
		return false
	}
	return true
}

// gRPC handler for updating the leader after 
func (s *CacheServer) UpdateLeader(ctx context.Context, request *pb.NewLeaderAnnouncement) (*pb.GenericResponse, error) {
	s.leader_id = request.LeaderId
	s.decision_chan <- s.leader_id
	return &pb.GenericResponse{Data: SUCCESS}, nil
}

// Return current status of this node (leader/follower)
func (s *CacheServer) GetHeartbeat(ctx context.Context, request *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	var status string 
	if s.node_id == s.leader_id {
		status = LEADER
	} else {
		status = FOLLOWER
	}
	s.logger.Infof("Node %d returning status %s to node %d", s.node_id, status, request.CallerNodeId)
	return &pb.HeartbeatResponse{Status: status, NodeId: s.node_id}, nil
}

// gRPC handler that receives a request with the caller's PID and returns its own PID. 
// If the PID is higher than the caller PID, we take over the election process.
func (s *CacheServer) GetPid(ctx context.Context, request *pb.PidRequest) (*pb.PidResponse, error) {
	local_pid := int32(os.Getpid())
	if local_pid > request.CallerPid {
		go s.RunElection()
	}
	return &pb.PidResponse{Pid: local_pid}, nil
}

// gRPC handler which allows other nodes to ask this node to start a new election
func (s *CacheServer) RequestElection(ctx context.Context, request *pb.ElectionRequest) (*pb.GenericResponse, error) {
	// asynchronously run election and return successful response
	go s.RunElection()
	return &pb.GenericResponse{Data: SUCCESS}, nil
}