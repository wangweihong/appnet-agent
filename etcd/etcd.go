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

	resp, err := c.Get(context.Background, paramDirNode, &client.GetOptions{Recursive})
	if err != nil {
		log.Error("Unabled to get key %v 's value: %v ", paramDirNode, err)
	}

	log.Debug("resp:%v", resp)

}

func (c *EtcdClient) GetNetworks(ip, network string) ([]byte, error) {
	if len(ip) == 0 || len(network) == 0 {
		log.Error("ip or network is empty")
		return []byte{}, fmt.Errorf("ip or network is empty")
	}

	key := networkDirNode + "/" + network + "/" + ip
	//试试
	resp, err := c.Get(context.Background(), key, client.GetOptions{Recursive: true})
	if err != nil {
		return []byte{}, err
	}

	log.Debug("resp:%v", resp)
	return []byte{}, nil
}

func (c *EtcdClient) UpdateData(key string, data []byte) {
	c.Set(context.Background(), key, data)
}

//这里需要放置在docker包里处理
func (c *EtcdClient) HandleNetworkEvent() {
	var resp client.Response

	switch resp.Action {
	case "create":
		var opts fsouza.NetworkConnectionOptions
		err := json.Unmarshal([]byte(resp.Node.Value), &opts)
		log.Debug("opts")

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
