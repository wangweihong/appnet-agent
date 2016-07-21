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
	*fsouza.Network
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

	log.Debug("%v", pool)

	net, exists := pool.Networks[nid]
	if !exists {
		log.Error("network %v doesn't exist", nid)
		return fmt.Errorf("network %v doesn't exist", nid)
	}

	if len(net.Containers) != 0 {
		log.Error("network %v still has associated containers", nid)
		return fmt.Errorf("network %v still has associated containers", nid)
	}

	delete(pool.Networks, nid)

	err := daemon.Client.RemoveNetwork(nid)
	if err != nil {
		log.Error("unable to remove %v for %v", nid, err)
		return fmt.Errorf("unable to remove %v for %v", nid, err)
	}

	//同步etcd上的数据..
	return nil
}

func (pool VirNetworkPool) CreateNetwork(opt fsouza.CreateNetworkOptions) error {
	pool.Lock()
	defer pool.Unlock()

	log.Debug("%v", pool)

	_, exists := pool.Networks[nid]
	if !exists {
		log.Error("network %v has exist", nid)
		return fmt.Errorf("network %v has exist", nid)
	}

	newnet, err := daemon.Client.CreateNetwork(opt)
	if err != nil {
		log.Error("unable to create %v for %v", nid, err)
		return fmt.Errorf("unable to create %v for %v", nid, err)
	}

	fullnet, err := daemon.Client.NetworkInfo(newnet.ID)

	pool.Networks[newnet.ID] = fullnet
	//同步etcd上的数据..
	return nil
}
