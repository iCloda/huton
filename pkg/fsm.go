package huton

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"go.etcd.io/etcd/raft/raftpb"
)

func (i *Instance) ApplyEntry(entry *raftpb.Entry) {
	var cacheSet CacheSet
	err := proto.Unmarshal(entry.Data, &cacheSet)
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	cache, err := i.Cache(cacheSet.CacheName)
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	if err := cache.executeSet(cacheSet.Key, cacheSet.Value); err != nil {
		fmt.Println("ERR:", err)
	}
}

func (i *Instance) Restore(snapshot *raftpb.Snapshot) {

}
