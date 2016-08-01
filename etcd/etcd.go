package etcd

import (
	"encoding/json"
	"fmt"
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

	ErrClusterUnavailable = client.ErrClusterUnavailable

	RegisterNodeTTL = 5 * time.Second
)

type EtcdNetworkEvent struct {
	*client.Response
}

type EtcdNetworkParamEvent struct {
	*client.Response
}

type EtcdClient struct {
	client.KeysAPI
}

func InitEtcdClient(endpoint string) *EtcdClient {
	if !strings.HasPrefix(endpoint, "http://") {
		endpoint = "http://" + endpoint
	}

	cfg := client.Config{
		Endpoints: []string{endpoint}, Transport: client.DefaultTransport,
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

func (c *EtcdClient) GetNetworkParam(network string) ([]byte, error) {
	key := paramDirNode + "/" + network
	resp, err := c.Get(context.Background(), key, nil)
	if err != nil {
		return []byte{}, err
	}

	return []byte(resp.Node.Value), nil
}

//更改为获取所有macvlan网络创建信息
func (c *EtcdClient) GetAllNetworkCreateParams() ([]string, error) {
	//	key := networkDirNode
	key := paramDirNode
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		e := err.(client.Error)
		if e.Code == client.ErrorCodeKeyNotFound {
			return []string{}, nil
		}
		return []string{}, err
	}

	var macvlanNetworks []string
	macvlanNetworks = make([]string, 0)

	for _, j := range resp.Node.Nodes {
		//需要的是网络名
		//	log.Logger.Debug("macvlan network in etcd :%v", j.Key)
		macvlanNetworks = append(macvlanNetworks, j.Value)
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
	if len(resp.Node.Value) == 0 {
		return nil, nil
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
	resp, err := c.Update(context.Background(), key, string(data))
	if err != nil {
		return err
	}

	log.Logger.Debug("resp:%v", resp)
	return nil
}

func (c *EtcdClient) CreateNetworkData(ip, network string, data []byte) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("updateNetworkData ip:%v,network:%v,data:%v,key:%v", ip, network, string(data), key)
	resp, err := c.Set(context.Background(), key, string(data), nil)
	if err != nil {
		return err
	}

	log.Logger.Debug("resp:%v", resp)
	return nil
}

//agent主机将自己的ip注册到etcd中
//问:etcd有没有机制检测多少时间内有更新节点?
//答:通过设置节点的TTL,agent必须在指定时间内更新/重新创建节点,一旦超时，节点丢失。
//可以认为该节点死亡.
//resp中有剩余的TTL时间
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
