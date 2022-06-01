package server

import (
	"context"
	empty "github.com/golang/protobuf/ptypes/empty"
	"github.com/malwaredllc/minicache/pb"
	"os"
	"time"
)

const (
	ELECTION_RUNNING    = true
	NO_ELECTION_RUNNING = false
	NO_LEADER           = "NO LEADER"
)

// Run an election using the Bully Algorithm (https://en.wikipedia.org/wiki/Bully_algorithm)
func (s *CacheServer) RunElection() {
	// an individual node should run a single election process, not multiple concurrent ones
	s.electionLock.Lock()
	if s.electionStatus == ELECTION_RUNNING {
		s.logger.Info("Election already running, waiting for completion...")
		return
	}

	// update status to election running
	s.electionStatus = ELECTION_RUNNING
	s.electionLock.Unlock()

	// check status of every node
	localPID := int32(os.Getpid())
	s.logger.Infof("Running election. Local PID: %d", localPID)

	for _, node := range s.nodesConfig.Nodes {
		// skip self
		if node.Id == s.nodeID {
			continue
		}

		// new identity service client
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// make status request rpc
		c, err := s.NewCacheClient(node.Host, int(node.GrpcPort))
		if err != nil {
			s.logger.Infof("error creating grpc client to node node %s: %v", node.Id, err)
		}
		res, err := c.GetPid(ctx, &pb.PidRequest{CallerPid: localPID})
		if err != nil {
			s.logger.Infof("PID request to node %s failed", node.Id)
			continue
		}

		// if response has a higher PID (use node id as tie-breaker), we send it an election request and wait to receive the election winner announcement.
		s.logger.Infof("Received PID %d from node %s (vs local PID %d on node %s)", res.Pid, node.Id, localPID, s.nodeID)
		if (localPID < res.Pid) || (res.Pid == localPID && s.nodeID < node.Id) {

			s.logger.Infof("Sending election request to node %s", node.Id)

			c, err := s.NewCacheClient(node.Host, int(node.GrpcPort))
			if err != nil {
				s.logger.Infof("error creating grpc client to node node %s: %v", node.Id, err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			_, err = c.RequestElection(ctx, &pb.ElectionRequest{CallerPid: localPID, CallerNodeId: s.nodeID})
			if err != nil {
				s.logger.Infof("Error requesting node %s run an election: %v", node.Id, err)
			}

			s.logger.Info("Waiting for decision...")
			// if after 5 seconds we receive no winner announcement, start the election process over
			select {
			case winner := <-s.decisionChan:
				if winner != "" {
					s.leaderID = winner
					s.logger.Infof("Received decision: Leader is node %s", s.leaderID)

					s.electionLock.Lock()
					s.electionStatus = NO_ELECTION_RUNNING
					s.electionLock.Unlock()
					return
				}
			case <-time.After(5 * time.Second):
				s.logger.Info("Timed out waiting for decision. Starting new election.")
				s.RunElection()

				s.electionLock.Lock()
				s.electionStatus = NO_ELECTION_RUNNING
				s.electionLock.Unlock()
				return
			}
		}
	}
	// if no other nodes have a higher PID, we are the winner
	s.leaderID = s.nodeID
	s.logger.Infof("set leader as self: %s", s.nodeID)

	// announce ourselves as winner to other nodes
	s.AnnounceNewLeader(s.leaderID)

	// reset election status
	s.electionLock.Lock()
	s.electionStatus = NO_ELECTION_RUNNING
	s.electionLock.Unlock()
}

// Announce new leader to all nodes
func (s *CacheServer) AnnounceNewLeader(winner string) {
	s.logger.Infof("Announcing node %s won election", winner)

	// if no response from any higher node IDs, declare self the winner and announce to all
	for _, node := range s.nodesConfig.Nodes {
		// skip self
		if node.Id == s.nodeID {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		// make status request rpc
		c, err := s.NewCacheClient(node.Host, int(node.GrpcPort))
		if err != nil {
			s.logger.Infof("error creating grpc client to node node %s: %v", node.Id, err)
		}

		_, err = c.UpdateLeader(ctx, &pb.NewLeaderAnnouncement{LeaderId: winner})
		if err != nil {
			s.logger.Infof("Election winner announcement to node %s error: %v", node.Id, err)
		}
		cancel()
	}
}

// Returns current leader
func (s *CacheServer) GetLeader(ctx context.Context, request *pb.LeaderRequest) (*pb.LeaderResponse, error) {
	// while there is no leader, run election
	for {
		if s.leaderID != NO_LEADER {
			break
		}
		s.RunElection()

		// if no leader was elected, wait 3 seconds then run another election
		if s.leaderID == NO_LEADER {
			s.logger.Info("No leader elected, waiting 3 seconds before trying again...")
			time.Sleep(3 * time.Second)
		}
	}
	return &pb.LeaderResponse{Id: s.leaderID}, nil
}

// Checks if leader is alive every 1 second. If no response for 3 seconds, new election is held.
func (s *CacheServer) StartLeaderHeartbeatMonitor() {
	// wait for decision to get leader
	s.logger.Info("Leader heartbeat monitor starting...")

	ticker := time.NewTicker(time.Second)
	for {
		// run heartbeat check every 1 second
		<-ticker.C

		// case 1: we are a follower
		if s.leaderID != s.nodeID {
			if !s.IsLeaderAlive() {
				s.logger.Info("Leader heartbeat failed, running new election")
				s.RunElection()
				s.logger.Info("Election done, leader heartbeat continuing")
			}

			select {
			case <-s.shutdownChan:
				s.logger.Info("Received shutdown signal")
				break
			case <-time.After(time.Second):
				continue
			}

			// case 2: we are the leader, so check for any dead nodes and remove them from cluster
		} else {
			modified := false
			for _, node := range s.nodesConfig.Nodes {
				// skip self
				if node.Id == s.nodeID {
					continue
				}
				// new identity service client
				c, err := s.NewCacheClient(node.Host, int(node.GrpcPort))
				if err != nil {
					s.logger.Infof("error creating grpc client to node node %s: %v", node.Id, err)
					delete(s.nodesConfig.Nodes, node.Id)
					modified = true
					continue
				}

				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()

				s.logger.Infof("Checking health of node %s", node.Id)
				_, err = c.GetHeartbeat(ctx, &pb.HeartbeatRequest{CallerNodeId: s.nodeID})
				if err != nil {
					s.logger.Infof("Node %s healthcheck returned error, removing from cluster", node.Id)
					delete(s.nodesConfig.Nodes, node.Id)
					modified = true
				}
			}

			// if cluster was modified, send out updated cluster config to other nodes
			if modified {
				s.logger.Info("Detected node config change, sending update to other nodes")
				s.updateClusterConfigInternal()
			}
		}
	}
}

// Check if leader node is alive (3 second timeout)
func (s *CacheServer) IsLeaderAlive() bool {
	// make sure leader exists
	if s.leaderID == NO_LEADER {
		s.logger.Infof("IsLeaderAlive found leader doesn't exist")
		return false
	}
	// if this node is the leader, return true
	if s.nodeID == s.leaderID {
		return true
	}
	s.logger.Infof("leader is %s", s.leaderID)
	leader, ok := s.nodesConfig.Nodes[s.leaderID]
	if !ok {
		s.logger.Infof("leader %s does not exist", s.leaderID)
		return true
	}

	// new identity service client
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// make status request rpc
	c, err := s.NewCacheClient(leader.Host, int(leader.GrpcPort))
	if err != nil {
		s.logger.Infof("error creating grpc client to node %s: %v", leader.Id, err)
		return false
	}

	_, err = c.GetHeartbeat(ctx, &pb.HeartbeatRequest{CallerNodeId: s.nodeID})
	if err != nil {
		s.logger.Infof("Leader healthcheck returned error: %v", err)
		return false
	}
	return true
}

// gRPC handler for updating the leader after
func (s *CacheServer) UpdateLeader(ctx context.Context, request *pb.NewLeaderAnnouncement) (*pb.GenericResponse, error) {
	s.logger.Infof("Received announcement leader is %s", request.LeaderId)
	s.leaderID = request.LeaderId
	s.decisionChan <- s.leaderID
	return &pb.GenericResponse{Data: SUCCESS}, nil
}

// Return current status of this node (leader/follower)
func (s *CacheServer) GetHeartbeat(ctx context.Context, request *pb.HeartbeatRequest) (*empty.Empty, error) {
	s.logger.Infof("Node %s returning heartbeat to node %s", s.nodeID, request.CallerNodeId)
	return &empty.Empty{}, nil
}

// gRPC handler that receives a request with the caller's PID and returns its own PID.
// If the PID is higher than the caller PID, we take over the election process.
func (s *CacheServer) GetPid(ctx context.Context, request *pb.PidRequest) (*pb.PidResponse, error) {
	local_pid := int32(os.Getpid())
	return &pb.PidResponse{Pid: local_pid}, nil
}

// gRPC handler which allows other nodes to ask this node to start a new election
func (s *CacheServer) RequestElection(ctx context.Context, request *pb.ElectionRequest) (*pb.GenericResponse, error) {
	// asynchronously run election and return successful response
	s.logger.Infof("received request for election from %s", request.CallerNodeId)
	go s.RunElection()
	return &pb.GenericResponse{Data: SUCCESS}, nil
}
