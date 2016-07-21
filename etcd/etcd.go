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
	macvlanDirNode = "/macvlan/"
	agentDirNode   = "/macvlan/nodes/"
	networkDirNode = "/macvlan/network/nodes"
	paramDirNode   = "/macvlan/network/params"

	//ErrClusterUnavailable = errors.New("client: etcd cluster is unavailable or misconfigured")
	ErrClusterUnavailable = client.ErrClusterUnavailable

	RegisterNodeTTL = 5 * time.Second
)

//节点：/appnet/macvlan/<macvlan name>/...

func (c *EtcdClient) ListenNetwork() {
	watcher := c.Watcher(networkDirNode, &client.WatcherOptions{Recursive: true})

	for {
		res, err := watcher.Next(context.Background())
		if err != nil {
			//	if err.Error() == client.ErrClusterUnavailable.Error() {
			log.Error("%v", err)
			time.Sleep(1 * time.Second)
			continue
			//}
		}

		switch res.Action {
		case "delete", "create", "update", "compareAndSwap", "compareAndDelete":
			log.Debug("%v(%v):%v", res.Node.Key, res.Node.Value, res.Action)
		}

	}
}

func (c *EtcdClient) GetNetworkParams() {

	key := paramDirNode
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		log.Error("Unabled to get key %v 's value: %v ", key, err)
	}

	log.Debug("resp:%v", resp)

}

func (c *EtcdClient) GetNetworkParam(network string) ([]byte, error) {
	if len(network) == 0 {
		log.Error("network is empty")
		return []byte{}, fmt.Errorf("network is empty")
	}

	key := paramDirNode + "/" + network
	resp, err := c.Get(context.Background, key, nil)
	if err != nil {
		return []byte{}, err
	}

	return []byte(resp.Node.Value), nil

}

//更改为获取所有macvlan网络名
func (c *EtcdClient) GetNetworks() ([]string, error) {

	key := networkDirNode
	//试试
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		return []string{}, err
	}

	log.Debug("resp:%v", resp)
	var macvlanNetworks []string

	for _, j := range resp.Node.Nodes {
		macvlanNetworks = append(macvlanNetworks, j.Key)
	}

	return macvlanNetworks, nil
}

//获取指定主机节点的指定macvlan的详细信息
func (c *EtcdClient) InfoNetwork(ip, network string) ([]string, error) {
	if len(ip) == 0 || len(network) == 0 {
		log.Error("ip or network is empty")
		return []string{}, fmt.Errorf("ip or network is empty")
	}
	key := networkDirNode + "/" + network + "/" + ip
	resp, err := c.Get(context.Background(), key, &client.GetOptions{Recursive: true})
	if err != nil {
		return []string{}, err
	}

	log.Debug("resp:%v", resp)

	//需要考虑转换成何种类型的数据
	return []string{}, nil

}

func (c *EtcdClient) UpdateNetworkData(ip, network, data []byte) error {
	key := networkDirNode + "/" + network + "/" + ip
	resp, err := e.Set(context.Background(), key, string(data), nil)
	if err != nil {
		return err
	}

	log.Debug("resp:%v", resp)
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
			log.Debug("opts fail:%v", err)
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
		log.Debug("%v", err)
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
	resp, err := e.Set(context.Background(), agentNode, ip, &client.SetOptions{TTL: RegisterNodeTTL})
	if err != nil {
		log.Error("register node fail:%v", err)
		return err
	}
	log.Debug("resp:%v", resp)
	return nil
}

func (e *EtcdClient) Save(node, key string) {

}
