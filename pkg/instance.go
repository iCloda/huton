package huton

import (
	"errors"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/huton-io/huton/pkg/swarm"
	context "golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/hashicorp/serf/serf"
)

const (
	raftRemoveGracePeriod = 5 * time.Second
)

var (
	// ErrNoName is an error used when and instance name is not provided
	ErrNoName = errors.New("no instance name provided")
)

type consensusGroup interface {
	Start(peers []string) error
	Stop() error
	Started() bool
	Propose(ctx context.Context, data []byte) error
	AddPeer(peer string) error
	RemovePeer(peer string) error
}

// Instance is an interface for the Huton instance.
type Instance struct {
	config           *Config
	logger           *log.Logger
	cgroup           consensusGroup
	serf             *serf.Serf
	rpc              *grpc.Server
	serfEventChannel chan serf.Event
	shutdownCh       chan struct{}
	peersMu          sync.Mutex
	peers            map[string]*Peer
	dbMu             sync.Mutex
	caches           map[string]*Cache
	raftNotifyCh     chan bool
	readyCh          chan struct{}
	shutdownLock     sync.Mutex
	shutdown         bool
	errCh            chan error
}

// Cache returns the cache with the given name. If no cache exists with the
// provided name, a new, empty cache with that name will be created.
func (i *Instance) Cache(name string) (*Cache, error) {
	i.dbMu.Lock()
	defer i.dbMu.Unlock()
	if c, ok := i.caches[name]; ok {
		return c, nil
	}
	dc, err := newCache(name, i.cgroup)
	if err != nil {
		return nil, err
	}
	i.caches[name] = dc
	return dc, nil
}

// Join joins and existing cluster.
func (i *Instance) Join(addrs []string) (int, error) {
	return i.serf.Join(addrs, true)
}

// Shutdown forcefully shuts down the instance.
func (i *Instance) Shutdown() error {
	// i.logger.Println("[INFO] Shutting down instance...")
	i.shutdownLock.Lock()
	defer i.shutdownLock.Unlock()
	if i.shutdown {
		return nil
	}
	i.shutdown = true
	close(i.shutdownCh)
	if i.serf != nil {
		i.serf.Shutdown()
	}
	return i.cgroup.Stop()
}

// NewInstance creates a new Huton instance and initializes it and all of its sub-components, such as Serf, Raft, and
// GRPC server, with the provided configuration.
//
// If this function returns successfully, the instance should be considered started and ready for use.
func NewInstance(config Config) (*Instance, error) {
	i := &Instance{
		config:           &config,
		rpc:              grpc.NewServer(),
		shutdownCh:       make(chan struct{}, 1),
		caches:           make(map[string]*Cache),
		serfEventChannel: make(chan serf.Event, 256),
		errCh:            make(chan error, 256),
		readyCh:          make(chan struct{}, 1),
	}
	i.ensureConfig()
	rpcAddr := net.JoinHostPort(i.config.BindHost, strconv.Itoa(i.config.BindPort+2))
	raftGroup := swarm.NewRaftGroup(rpcAddr, i.config.Replication, i)
	i.cgroup = raftGroup
	// i.logger.Println("[INFO] Initializing RPC server at address ", rpcAddr)
	if err := i.setupRPC(rpcAddr, raftGroup); err != nil {
		i.Shutdown()
		return nil, err
	}
	// i.logger.Println("[INFO] Initializing Serf cluster...")
	if err := i.setupSerf(rpcAddr); err != nil {
		i.Shutdown()
		return i, err
	}
	go i.handleEvents()
	return i, nil
}

func (i *Instance) ensureConfig() {
	if i.config.BindHost == "" {
		i.config.BindHost = "127.0.0.1"
	}
	if i.config.LogOutput == nil {
		i.config.LogOutput = os.Stdout
	}
	if i.config.Expect < 3 {
		i.config.Expect = 3
	}
	if i.config.Replication.Transport == nil {
		i.config.Replication.Transport = swarm.NewGRPCTransport()
	}
	if i.config.Replication.TickInterval == 0 {
		i.config.Replication.TickInterval = time.Second
	}
	i.logger = log.New(os.Stdout, "huton", log.LstdFlags)
}

func (i *Instance) handleEvents() {
	for {
		select {
		case e := <-i.serfEventChannel:
			i.handleSerfEvent(e)
		case <-i.shutdownCh:
			return
		case err := <-i.errCh:
			i.logger.Printf("[ERR] %s", err)
		case <-i.shutdownCh:
			return
		}
	}
}
