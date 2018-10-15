package swarm

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.etcd.io/etcd/raft"
	"go.etcd.io/etcd/raft/raftpb"
)

// RaftFSM is used to apply raft entries and restoring application state from a raft snapshot.
type RaftFSM interface {
	// ApplyEntry applies application state from a Raft log entry.
	ApplyEntry(entry *raftpb.Entry)
	// Restore restores the application state from a Raft Snapshot.
	Restore(snapshot *raftpb.Snapshot)
}

// RaftTransport is used to send Raft messages to other nodes.
type RaftTransport interface {
	AddPeer(peer string, id uint64) error
	RemovePeer(id uint64) error
	Send(ctx context.Context, messages []raftpb.Message) error
}

// RaftConfig provides configuration options to the RaftGroup.
type RaftConfig struct {
	SnapDir      string
	TickInterval time.Duration
	Transport    RaftTransport
}

// RaftGroup represents a Raft consensus group that manages replication
// and consensus between nodes.
type RaftGroup struct {
	id        uint64
	config    RaftConfig
	raft      raft.Node
	storage   *raft.MemoryStorage
	ticker    *time.Ticker
	stopCh    chan bool
	readyCh   chan bool
	removedCh chan bool
	fsm       RaftFSM
}

// NewRaftGroup returns a new RaftGroup with the provided config
func NewRaftGroup(id string, config RaftConfig, fsm RaftFSM) *RaftGroup {
	return &RaftGroup{
		id:        HashPeer(id),
		config:    config,
		storage:   raft.NewMemoryStorage(),
		fsm:       fsm,
		stopCh:    make(chan bool),
		readyCh:   make(chan bool),
		removedCh: make(chan bool),
	}
}

// Start initializes the raft layer and begins processing raft events.
func (cg *RaftGroup) Start(peers []string) error {
	raftConfig := cg.getRaftConfig()
	peerList := make([]raft.Peer, len(peers))
	for i, peer := range peers {
		id := HashPeer(peer)
		peerList[i] = raft.Peer{ID: id, Context: []byte(peer)}
		// if err := cg.config.Transport.AddPeer(peer, id); err != nil {
		// 	return err
		// }
	}
	cg.raft = raft.StartNode(raftConfig, peerList)
	go cg.serveRaft()
	close(cg.readyCh)
	return nil
}

// Stop forces raft processing to quit. Once this is called, the RaftGroup
//is no longer valid. A a restart is necessary, a new RaftGroup should be created.
func (cg *RaftGroup) Stop() error {
	fmt.Println("shutting down raft")
	if cg.raft == nil {
		return nil
	}
	// Wait up to 5 seconds for our node to be removed from the raft cluster.
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	if err := cg.removeSelf(ctx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		fmt.Println("ctx done: ", ctx.Err())
		return ctx.Err()
	case <-cg.removedCh:
		fmt.Println("remove channel closed")
	}
	close(cg.stopCh)
	cg.raft.Stop()
	return nil
}

// Propose proposes a raft entry to the raft group for consensus.
func (cg *RaftGroup) Propose(ctx context.Context, data []byte) error {
	return cg.raft.Propose(ctx, data)
}

// Started returns true if the cluster has already been started.
func (cg *RaftGroup) Started() bool {
	select {
	case <-cg.readyCh:
		return true
	default:
		return false
	}
}

// AddPeer proposes a new peer to the raft group.
func (cg *RaftGroup) AddPeer(peer string) error {
	id := HashPeer(peer)
	return cg.raft.ProposeConfChange(context.Background(), raftpb.ConfChange{
		ID:      id,
		Type:    raftpb.ConfChangeAddNode,
		NodeID:  id,
		Context: []byte(peer),
	})
}

// RemovePeer proposes that a peer be removed from the raft group.
func (cg *RaftGroup) RemovePeer(peer string) error {
	id := HashPeer(peer)
	return cg.raft.ProposeConfChange(context.Background(), raftpb.ConfChange{
		Type:    raftpb.ConfChangeRemoveNode,
		NodeID:  id,
		Context: []byte(peer),
	})
}

// Send is a callback for when a Raft message is received via GRPC.
//
// Satisfies the RaftTransportServer interface.
func (cg *RaftGroup) Send(ctx context.Context, message *raftpb.Message) (*EmptyResponse, error) {
	err := cg.raft.Step(ctx, *message)
	return &EmptyResponse{}, err
}

func (cg *RaftGroup) getRaftConfig() *raft.Config {
	return &raft.Config{
		ID:              cg.id,
		ElectionTick:    10,
		HeartbeatTick:   1,
		Storage:         cg.storage,
		MaxSizePerMsg:   math.MaxUint16,
		MaxInflightMsgs: 256,
	}
}

func (cg *RaftGroup) serveRaft() {
	cg.ticker = time.NewTicker(cg.config.TickInterval)
	transport := cg.config.Transport
	fsm := cg.fsm
	for {
		select {
		case <-cg.ticker.C:
			cg.raft.Tick()
		case rd := <-cg.raft.Ready():
			cg.store(rd.HardState, rd.Entries, rd.Snapshot)
			transport.Send(context.Background(), rd.Messages)
			if !raft.IsEmptySnap(rd.Snapshot) {
				fsm.Restore(&rd.Snapshot)
			}
			for _, entry := range rd.CommittedEntries {
				cg.processEntry(entry)
				if entry.Type == raftpb.EntryConfChange {
					var cc raftpb.ConfChange
					cc.Unmarshal(entry.Data)
					cg.raft.ApplyConfChange(cc)
					switch cc.Type {
					case raftpb.ConfChangeRemoveNode:
						if err := transport.RemovePeer(cc.NodeID); err != nil {
							fmt.Println("failed to remove peer", err)
							// TODO: Log err
						}
						if cc.NodeID == cg.id {
							fmt.Println("removed ourself")
							close(cg.removedCh)
						}
					case raftpb.ConfChangeAddNode:
						if err := transport.AddPeer(string(cc.Context), cc.NodeID); err != nil {
							// TODO: Log err
						}
					}
				}
			}
			cg.raft.Advance()
		case <-cg.stopCh:
			return
		}
	}
}

func (cg *RaftGroup) processEntry(entry raftpb.Entry) {
	if entry.Type == raftpb.EntryNormal && entry.Data != nil {
		cg.fsm.ApplyEntry(&entry)
	}
}

func (cg *RaftGroup) store(hardState raftpb.HardState, entries []raftpb.Entry, snapshot raftpb.Snapshot) {
	if err := cg.storage.Append(entries); err != nil {
		// TODO: Do something with error
	}
	if !raft.IsEmptyHardState(hardState) {
		cg.storage.SetHardState(hardState)
	}
	if !raft.IsEmptySnap(snapshot) {
		cg.storage.ApplySnapshot(snapshot)
	}
}

func (cg *RaftGroup) removeSelf(ctx context.Context) error {
	return cg.raft.ProposeConfChange(ctx, raftpb.ConfChange{
		Type:   raftpb.ConfChangeRemoveNode,
		NodeID: cg.id,
	})
}
