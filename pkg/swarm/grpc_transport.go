package swarm

import (
	"context"
	"fmt"
	"sync"

	"go.etcd.io/etcd/raft/raftpb"
	"google.golang.org/grpc"
)

// GRPCTransport is a RaftTransport implementation that sends
// Raft messages via GRPC.
type GRPCTransport struct {
	connsMu sync.RWMutex
	conns   map[uint64]*grpc.ClientConn
}

// NewGRPCTransport returns a new GRPCTransport.
func NewGRPCTransport() *GRPCTransport {
	return &GRPCTransport{conns: make(map[uint64]*grpc.ClientConn)}
}

// AddPeer creates a new client connection for a peer if one doesn't already exist.
func (t *GRPCTransport) AddPeer(peer string, id uint64) error {
	conn, err := grpc.Dial(peer, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("failed to create grpc connection to %s: %s", peer, err)
	}
	t.connsMu.Lock()
	defer t.connsMu.Unlock()
	if _, ok := t.conns[id]; !ok {
		t.conns[id] = conn
	}
	return nil
}

// RemovePeer closes the connection of a peer if it exists
func (t *GRPCTransport) RemovePeer(id uint64) error {
	t.connsMu.Lock()
	defer t.connsMu.Unlock()
	if conn, ok := t.conns[id]; ok {
		if err := conn.Close(); err != nil {
			return err
		}
		delete(t.conns, id)
	}
	return nil
}

// Send sends a list a raft messages to
func (t *GRPCTransport) Send(ctx context.Context, messages []raftpb.Message) error {
	// Raft pb uses proto2 so when the proto is generated, it uses pointer types. Need to convert. :(
	t.connsMu.RLock()
	defer t.connsMu.RUnlock()
	for _, msg := range messages {
		if conn, ok := t.conns[msg.To]; ok {
			client := NewRaftTransportClient(conn)
			// The response is always going to be empty, so we don't really care about the response.
			if _, err := client.Send(ctx, &msg); err != nil {
				// fmt.Println("failed to send: ", err)
			}
		} else {
			// fmt.Println("no conn")
		}
	}
	return nil
}
