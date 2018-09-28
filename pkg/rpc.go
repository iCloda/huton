package huton

import (
	"net"

	"github.com/huton-io/huton/pkg/swarm"
)

func (i *Instance) setupRPC(addr string, transportServer swarm.RaftTransportServer) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	swarm.RegisterRaftTransportServer(i.rpc, transportServer)
	go i.rpc.Serve(lis)
	return nil
}
