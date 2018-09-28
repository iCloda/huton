package huton

import (
	"context"
	"errors"

	"github.com/couchbase/moss"
	proto "github.com/golang/protobuf/proto"
)

const (
	cacheOpSet byte = iota
	cacheOpDel
)

var (
	// ErrWrongBatchType is an error used when the provided implementation of Batch is unexpected.
	ErrWrongBatchType = errors.New("wrong batch implementation type")
)

// Cache is an in-memory key-value store.
type Cache struct {
	name       string
	cgroup     consensusGroup
	collection moss.Collection
}

// Name returns the name of the cache.
func (c *Cache) Name() string {
	return c.name
}

// Set sets the value for the given key. An error is returned if the value could not be set.
func (c *Cache) Set(key []byte, val []byte) error {
	data, err := proto.Marshal(&CacheSet{
		CacheName: c.name,
		Key:       key,
		Value:     val,
	})
	if err != nil {
		return err
	}
	return c.cgroup.Propose(context.Background(), data)
}

// Delete deletes the value for the given key. An error is returned if the value could not be deleted.
func (c *Cache) Delete(key []byte) error {
	data, err := proto.Marshal(&CacheDel{
		CacheName: c.name,
		Key:       key,
	})
	if err != nil {
		return err
	}
	return c.cgroup.Propose(context.Background(), data)
}

// Get retrieves the value for the given key. An error is returned if something goes wrong while retrieving the value.
// A value of nil means that the key was not found.
func (c *Cache) Get(key []byte) ([]byte, error) {
	ss, err := c.collection.Snapshot()
	if err != nil {
		return nil, err
	}
	return ss.Get(key, moss.ReadOptions{})
}

func (c *Cache) executeSet(key, value []byte) error {
	batch, err := c.collection.NewBatch(1, len(key)+len(value)+16)
	if err != nil {
		return err
	}
	if err := batch.Set(key, value); err != nil {
		return err
	}
	return c.collection.ExecuteBatch(batch, moss.WriteOptions{})
}

func (c *Cache) executeDelete(key []byte) error {
	batch, err := c.collection.NewBatch(1, len(key))
	if err != nil {
		return err
	}
	if err := batch.Del(key); err != nil {
		return err
	}
	return c.collection.ExecuteBatch(batch, moss.WriteOptions{})
}

func newCache(name string, cgroup consensusGroup) (*Cache, error) {
	col, err := moss.NewCollection(moss.CollectionOptions{})
	if err != nil {
		return nil, err
	}
	c := &Cache{
		name:       name,
		cgroup:     cgroup,
		collection: col,
	}
	err = c.collection.Start()
	return c, err
}
