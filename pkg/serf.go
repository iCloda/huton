package huton

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"

	"github.com/hashicorp/serf/serf"
)

func (i *Instance) setupSerf(rpcAddr string) (err error) {
	serfConfig, err := i.getSerfConfig(rpcAddr)
	if err != nil {
		return err
	}
	i.serf, err = serf.Create(serfConfig)
	return err
}

func (i *Instance) getSerfConfig(rpcAddr string) (*serf.Config, error) {
	serfConfig := serf.DefaultConfig()
	serfConfig.LogOutput = ioutil.Discard
	serfConfig.MemberlistConfig.LogOutput = ioutil.Discard
	serfConfig.EnableNameConflictResolution = false
	serfConfig.MemberlistConfig.BindAddr = i.config.BindHost
	serfConfig.MemberlistConfig.BindPort = i.config.BindPort
	serfConfig.NodeName = rpcAddr
	serfConfig.EventCh = i.serfEventChannel
	encKey := i.config.SerfEncryptionKey
	if encKey != nil && len(encKey) != 32 {
		return serfConfig, errors.New("invalid encryption key length. Encryption key must be 32-bytes")
	}
	serfConfig.MemberlistConfig.SecretKey = encKey
	tags := make(map[string]string)
	tags["id"] = serfConfig.NodeName
	tags["rpcAddr"] = rpcAddr
	if i.config.Bootstrap {
		tags["bootstrap"] = "1"
	}
	tags["expect"] = strconv.Itoa(i.config.Expect)
	serfConfig.Tags = tags
	return serfConfig, nil
}

func (i *Instance) handleSerfEvent(event serf.Event) {
	switch event.EventType() {
	case serf.EventMemberJoin:
		i.peerJoined(event.(serf.MemberEvent))
	case serf.EventMemberLeave, serf.EventMemberFailed:
		i.peerGone(event.(serf.MemberEvent))
	default:
		i.logger.Printf("[WARN] unhandled serf event: %#v", event)
	}
}

func (i *Instance) peerJoined(event serf.MemberEvent) {
	// i.logger.Println("[INFO] member join")
	for _, member := range event.Members {
		if i.config.Expect > 0 {
			i.maybeBootstrap()
		} else {
			// TODO: Handle the case where a cluster already exists, but still needs started.
			if err := i.cgroup.AddPeer(member.Tags["rpcAddr"]); err != nil {
				fmt.Println("failed to add peer: ", err)
			}
		}
	}
}

func (i *Instance) peerGone(event serf.MemberEvent) {
	for _, member := range event.Members {
		i.cgroup.RemovePeer(member.Tags["rpcAddr"])
	}
}

func (i *Instance) maybeBootstrap() {
	members := i.serf.Members()
	if len(members) < i.config.Expect {
		return
	}
	addrs := make([]string, len(members))
	for i, member := range members {
		addrs[i] = member.Tags["rpcAddr"]
	}
	// i.logger.Printf("[INFO] found expected number of peers, attempting boostrap: %s", strings.Join(addrs, ","))
	if err := i.cgroup.Start(addrs); err != nil {
		i.logger.Printf("[ERR] failed to bootstrap cluster: %v", err)
		return
	}
	i.config.Expect = 0
}
