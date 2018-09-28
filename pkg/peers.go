package huton

import (
	"encoding/json"
	"net"
	"strconv"

	"github.com/hashicorp/serf/serf"
)

// Peer contains information about a cluster member.
type Peer struct {
	ID        string
	SerfAddr  *net.TCPAddr
	Expect    int
	Bootstrap bool
}

func (p *Peer) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func newPeer(member serf.Member) (*Peer, error) {
	expect, err := strconv.Atoi(member.Tags["expect"])
	if err != nil {
		return nil, err
	}
	return &Peer{
		ID: member.Tags["id"],
		SerfAddr: &net.TCPAddr{
			IP:   member.Addr,
			Port: int(member.Port),
		},
		Expect:    expect,
		Bootstrap: member.Tags["boostrap"] == "1",
	}, nil
}
