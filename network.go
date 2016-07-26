package main

import (
	"fmt"
	"sync"

	fsouza "github.com/fsouza/go-dockerclient"

	"appnet-agent/daemon"
	"appnet-agent/log"
)

type VirNetworkPool struct {
	lock     *sync.RWMutex
	Networks map[string]VirNetwork `json:"networks"`
}

type VirNetwork struct {
	fsouza.Network
}

func InitPool() *VirNetworkPool {
	pool := VirNetworkPool{}
	pool.lock = new(sync.RWMutex)
	pool.Networks = make(map[string]VirNetwork)

	return &pool
}

//需要放在这里吗?
func (pool VirNetworkPool) SyncEtcd() {
}

func (pool VirNetworkPool) Lock() {
	pool.lock.Lock()
}

func (pool VirNetworkPool) Unlock() {
	pool.lock.Unlock()
}

func (pool VirNetworkPool) RemoveNetwork(nid string) error {
	pool.Lock()
	defer pool.Unlock()

	log.Logger.Debug("%v", pool)

	net, exists := pool.Networks[nid]
	if !exists {
		log.Logger.Error("network %v doesn't exist", nid)
		return fmt.Errorf("network %v doesn't exist", nid)
	}

	if len(net.Containers) != 0 {
		log.Logger.Error("network %v still has associated containers", nid)
		return fmt.Errorf("network %v still has associated containers", nid)
	}

	delete(pool.Networks, nid)

	//FIXME
	err := daemon.Client.RemoveNetwork(nid)
	if err != nil {
		log.Logger.Error("unable to remove %v for %v", nid, err)
		return fmt.Errorf("unable to remove %v for %v", nid, err)
	}

	return nil
}

func (pool VirNetworkPool) CreateNetwork(opt fsouza.CreateNetworkOptions) error {
	pool.Lock()
	defer pool.Unlock()

	log.Logger.Debug("%v", pool)

	newnet, err := daemon.Client.CreateNetwork(opt)
	if err != nil {
		log.Logger.Error("unable to create %v for %v", opt.Name, err)
		return fmt.Errorf("unable to create %v for %v", opt.Name, err)
	}

	fullnet, err := daemon.Client.NetworkInfo(newnet.ID)

	virtnet := VirNetwork{*fullnet}
	//FIXME:移除出去?
	pool.Networks[newnet.ID] = virtnet
	return nil
}
