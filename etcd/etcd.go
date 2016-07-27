package etcd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"appnet-agent/log"

	"github.com/coreos/etcd/client"
	fsouza "github.com/fsouza/go-dockerclient"
	"golang.org/x/net/context"
)

var (
	macvlanDirNode = "/macvlan"
	//保存所有主机节点IP
	agentDirNode = macvlanDirNode + "/nodes/"
	//保持着所有虚拟网络中对应的真实macvlan的详细信息
	//如/macvlan/network/nodes/test123/192.168.14.1 表示的是test123这个macvlan虚拟网络在192.168.14.1主机节点的信息
	networkDirNode = macvlanDirNode + "/network/nodes"
	//保存所有macvlan网络的创建参数，一旦移除，意味着移除网络
	paramDirNode = macvlanDirNode + "/network/params"
	//保存着虚拟macvlan网络的信息，是各个主机节点上的真实macvlan网络的抽象
	clusterNode = macvlanDirNode + "/cluster"

	//ErrClusterUnavailable = errors.New("client: etcd cluster is unavailable or misconfigured")
	ErrClusterUnavailable = client.ErrClusterUnavailable

	RegisterNodeTTL = 5 * time.Second
)

type EtcdNetworkEvent struct {
	*client.Response
}

type NetworkParam struct {
	*fsouza.CreateNetworkOptions
}

type EtcdNetworkParamEvent struct {
	*client.Response
}

//TODO:需要返回一个只包含网络/容器信息的结构体
func (c *EtcdClient) EtcdListenNetwork() <-chan EtcdNetworkEvent {
	dests := make(chan EtcdNetworkEvent)
	watcher := c.Watcher(networkDirNode, &client.WatcherOptions{Recursive: true})

	//异步监听
	go func() {
		for {
			res, err := watcher.Next(context.Background())
			if err != nil {
				log.Logger.Error("%v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			//相关的网络

			log.Logger.Debug("action==resp :%v", res)
			switch res.Action {
			case "delete", "create", "update", "compareAndSwap", "compareAndDelete":
				log.Logger.Debug("%v(%v):%v", res.Node.Key, res.Node.Value, res.Action)
				response := EtcdNetworkEvent{res}
				dests <- response
			}
		}
	}()

	return dests
}

func (c *EtcdClient) EtcdListenNetworkParam() <-chan EtcdNetworkParamEvent {
	dests := make(chan EtcdNetworkParamEvent)
	watcher := c.Watcher(paramDirNode, &client.WatcherOptions{Recursive: true})

	//异步监听
	go func() {
		for {
			res, err := watcher.Next(context.Background())
			if err != nil {
				log.Logger.Error("%v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			//相关的网络

			log.Logger.Debug("action==resp :%v", res)
			switch res.Action {
			case "delete", "create", "update", "compareAndSwap", "compareAndDelete", "set":
				log.Logger.Debug("key:%v(value:%v):action:%v", res.Node.Key, res.Node.Value, res.Action)
				response := EtcdNetworkParamEvent{res}
				dests <- response
			}
		}
	}()

	return dests

}

/*
func (c *EtcdClient) GetNetworkParams() {

	key := paramDirNode
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		log.Logger.Error("Unabled to get key %v 's value: %v ", key, err)
	}

	log.Logger.Debug("resp:%v", resp)
}
*/

func (c *EtcdClient) GetNetworkParam(network string) ([]byte, error) {
	key := paramDirNode + "/" + network
	resp, err := c.Get(context.Background(), key, nil)
	if err != nil {
		return []byte{}, err
	}

	return []byte(resp.Node.Value), nil
}

//更改为获取所有macvlan网络名
func (c *EtcdClient) GetNetworks() ([]string, error) {
	key := networkDirNode
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		return []string{}, err
	}

	log.Logger.Debug("resp:%v", resp)
	var macvlanNetworks []string

	for _, j := range resp.Node.Nodes {
		//需要的是网络名
		macvlanNetworks = append(macvlanNetworks, filepath.Base(j.Key))
	}

	return macvlanNetworks, nil
}

//获取指定主机节点的指定macvlan的详细信息
func (c *EtcdClient) InfoNetwork(ip, network string) (*fsouza.Network, error) {
	if len(ip) == 0 || len(network) == 0 {
		log.Logger.Error("ip or network is empty")
		return nil, fmt.Errorf("ip or network is empty")
	}
	key := networkDirNode + "/" + network + "/" + ip
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		//有可能该agent并没有成功创建macvlan网络，因此并没有对应的key存在。
		e := err.(client.Error)
		if e.Code == client.ErrorCodeKeyNotFound {
			return nil, nil
		}
		return nil, err
	}

	log.Logger.Debug("resp:%v", resp)

	var netinfo fsouza.Network
	err = json.Unmarshal([]byte(resp.Node.Value), &netinfo)
	if err != nil {
		log.Logger.Debug("unable to unmarshal json : %v ", err)
		return nil, err
	}

	//需要考虑转换成何种类型的数据
	return &netinfo, nil
}

func (c *EtcdClient) RemoveNetworkData(ip, network string) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("RemoveNetworkData ip:%v,network:%v,key:%v", ip, network, key)

	_, err := c.Delete(context.Background(), key, &client.DeleteOptions{})
	//FIXME:怎么处理
	if err != nil {
		log.Logger.Error("unable to remove macvlan key %v : %v", key, err)
		return err
	}
	return nil
}

func (c *EtcdClient) UpdateNetworkData(ip, network string, data []byte) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("updateNetworkData ip:%v,network:%v,data:%v,key:%v", ip, network, string(data), key)
	resp, err := c.Set(context.Background(), key, string(data), nil)
	if err != nil {
		return err
	}

	log.Logger.Debug("resp:%v", resp)
	return nil
}

func (c *EtcdClient) UpdateClusterData(env, clusterNetworkID string, data []byte) error {
	log.Logger.Debug("updateCluterData env:%v,clusterNetworkID:%v, data:%v", env, clusterNetworkID, string(data))
	key := clusterNode + "/" + env + "/" + clusterNetworkID
	_, err := c.Set(context.Background(), key, string(data), nil)
	if err != nil {

		log.Logger.Debug("UpdateClusterData fail for:%v", err)
		return err
	}
	return nil
}

func (c *EtcdClient) RemoveClusterData(env, clusterNetworkID string) error {
	log.Logger.Debug("removeCluterData env:%v,clusterNetworkID:%v", env, clusterNetworkID)
	key := clusterNode + "/" + env + "/" + clusterNetworkID
	_, err := c.Delete(context.Background(), key, nil)
	if err != nil {

		log.Logger.Debug("removeClusterData fail for:%v", err)
		return err

	}
	return nil
}

//这里需要放置在docker包里处理
func (c *EtcdClient) HandleNetworkEvent() {
	var resp client.Response

	switch resp.Action {
	case "create":
		var opts fsouza.NetworkConnectionOptions
		err := json.Unmarshal([]byte(resp.Node.Value), &opts)
		if err != nil {
			log.Logger.Debug("opts fail:%v", err)
		}

	case "delete":
	case "compareAndSwap":
	case "compareAndDelete":

	}
}

type EtcdClient struct {
	client.KeysAPI
}

func InitEtcdClient(endpoint string) *EtcdClient {
	if !strings.HasPrefix(endpoint, "http://") {
		endpoint = "http://" + endpoint
	}

	cfg := client.Config{
		Endpoints:               []string{endpoint},
		Transport:               client.DefaultTransport,
		HeaderTimeoutPerRequest: 10 * time.Second,
	}

	c, err := client.New(cfg)
	if err != nil {
		log.Logger.Debug("%v", err)
		return nil
	}
	kapi := client.NewKeysAPI(c)

	etcdClient := EtcdClient{
		kapi,
	}
	return &etcdClient
}

//agent主机将自己的ip注册到etcd中
//etcd有没有机制检测多少时间内有更新节点?
//通过设置节点的TTL,agent必须在指定时间内更新/重新创建节点,一旦超时，节点丢失。
//可以认为该节点死亡.
//怎么知道节点的expire time
//resp中有剩余的时间
func (e *EtcdClient) RegisterNode(ip string) error {
	agentNode := agentDirNode + "/" + ip
	_, err := e.Set(context.Background(), agentNode, ip, &client.SetOptions{TTL: RegisterNodeTTL})
	//	resp, err := e.Set(context.Background(), agentNode, ip, &client.SetOptions{TTL: RegisterNodeTTL})
	if err != nil {
		log.Logger.Error("register node fail:%v", err)
		return err
	}
	//	log.Logger.Debug("resp:%v", resp)
	return nil
}

func (e *EtcdClient) Save(node, key string) {

}
