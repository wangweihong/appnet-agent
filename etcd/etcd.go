package etcd

import (
	"encoding/json"
	"strings"
	"time"

	"appnet-agent/log"

	"github.com/coreos/etcd/client"
	fsouza "github.com/fsouza/go-dockerclient"
	"golang.org/x/net/context"
)

var (
	MacvlanDirNode = "/macvlan"
	//保存所有主机节点IP
	agentDirNode = MacvlanDirNode + "/nodes/"
	//保持着所有虚拟网络中对应的真实macvlan的详细信息
	//如/macvlan/network/nodes/test123/192.168.14.1 表示的是test123这个macvlan虚拟网络在192.168.14.1主机节点的信息
	networkDirNode = MacvlanDirNode + "/network/nodes"
	//保存所有macvlan网络的创建参数，一旦移除，意味着移除网络 paramDirNode = macvlanDirNode + "/network/params"
	paramDirNode = MacvlanDirNode + "/network/params"
	//保存着虚拟macvlan网络的信息，是各个主机节点上的真实macvlan网络的抽象
	clusterNode = MacvlanDirNode + "/cluster"

	//维护agent节点和节点所有容器信息
	appnetDirNode      = "/appnet"
	agentContainerNode = appnetDirNode + "/containers"

	//action由appnet设置, agent监听，其中可以为××/192.168.12.12.agent忽略非本机的节点
	//节点值为一个{action:"connect/disconnect", containerid:"id", networkid:""}
	//根据该值进行连接/断开操作
	//结果保存在/result节点中，由appnet监听，移除结果节点以及action节点.
	containerActionNode = appnetDirNode + "/container" + "/action"
	containerResultNode = appnetDirNode + "/container" + "/result"

	ErrClusterUnavailable = client.ErrClusterUnavailable

	RegisterNodeTTL = 5 * time.Second

	ResultTTL  = 5 * time.Second
	etcdClient *EtcdClient
)

type EtcdNetworkEvent struct {
	*client.Response
}

type EtcdNetworkParamEvent struct {
	*client.Response
}

type EtcdNetworkContainerAction struct {
	*client.Response
}

type EtcdClient struct {
	client.KeysAPI
}

func InitEtcdClient(endpoint string) error {
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
		return err
	}
	kapi := client.NewKeysAPI(c)

	etcdClient = &EtcdClient{
		kapi,
	}
	return nil
}

//TODO:需要返回一个只包含网络/容器信息的结构体
func ListenNetwork() <-chan EtcdNetworkEvent {
	key := networkDirNode
	dests := make(chan EtcdNetworkEvent)
	watcher := etcdClient.Watcher(key, &client.WatcherOptions{Recursive: true})

	//异步监听
	go func(dests chan EtcdNetworkEvent) {
		for {
			res, err := watcher.Next(context.Background())
			if err != nil {
				log.Logger.Debug("%v", err)
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
	}(dests)

	return dests
}

func ListenNetworkParam() <-chan EtcdNetworkParamEvent {
	key := paramDirNode
	dests := make(chan EtcdNetworkParamEvent)
	watcher := etcdClient.Watcher(key, &client.WatcherOptions{Recursive: true})

	//异步监听
	go func(dests chan EtcdNetworkParamEvent) {
		for {
			res, err := watcher.Next(context.Background())
			if err != nil {
				log.Logger.Debug("%v", err)
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
	}(dests)
	return dests
}

func ListenNetworkContainerEvent() <-chan EtcdNetworkContainerAction {
	key := containerActionNode
	dests := make(chan EtcdNetworkContainerAction)
	watcher := etcdClient.Watcher(key, &client.WatcherOptions{Recursive: true})

	go func(dests chan EtcdNetworkContainerAction) {
		for {
			res, err := watcher.Next(context.Background())
			if err != nil {
				log.Logger.Debug("listen netwrok container action fail:%v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			log.Logger.Debug("resp:%v", res)
			response := EtcdNetworkContainerAction{res}
			dests <- response
		}
	}(dests)
	return dests
}

func GetNetworkParam(network string) (*fsouza.CreateNetworkOptions, error) {
	key := paramDirNode + "/" + network
	resp, err := etcdClient.etcdGet(key, nil)
	if err != nil {
		return nil, err
	}

	var param fsouza.CreateNetworkOptions
	err = json.Unmarshal([]byte(resp.Node.Value), &param)
	if err != nil {
		log.Logger.Error("unable to unmarshal (%v): %v", resp.Node.Value, err)
		return nil, err
	}
	return &param, nil
}

//更改为获取所有macvlan网络创建信息
//func (c *EtcdClient) GetAllNetworkCreateParams() ([]string, error) {
func GetAllNetworkCreateParams() ([]fsouza.CreateNetworkOptions, error) {
	//	key := networkDirNode
	key := paramDirNode
	resp, err := etcdClient.etcdGet(key, &client.GetOptions{Recursive: true})
	if err != nil {
		e := err.(client.Error)
		if e.Code == client.ErrorCodeKeyNotFound {
			return nil, nil
		}
		return nil, err
	}

	var macvlanNetworks []string
	etcdNetworkParams := make([]fsouza.CreateNetworkOptions, 0)
	macvlanNetworks = make([]string, 0)

	for _, j := range resp.Node.Nodes {
		//需要的是网络名
		//	log.Logger.Debug("macvlan network in etcd :%v", j.Key)
		macvlanNetworks = append(macvlanNetworks, j.Value)
	}

	for _, v := range macvlanNetworks {
		var param fsouza.CreateNetworkOptions
		err := json.Unmarshal([]byte(v), &param)
		if err != nil {
			log.Logger.Error("json unmarshal network param fail:%v", err)
			return nil, err
		}

		etcdNetworkParams = append(etcdNetworkParams, param)
	}

	return etcdNetworkParams, nil
}

//获取指定主机节点的指定macvlan的详细信息
func InfoNetwork(ip, network string) (*fsouza.Network, error) {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("key:%v", key)
	resp, err := etcdClient.etcdGet(key, &client.GetOptions{Recursive: true})
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

func RemoveNetworkData(ip, network string) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("RemoveNetworkData ip:%v,network:%v,key:%v", ip, network, key)

	_, err := etcdClient.etcdDelete(key, &client.DeleteOptions{})
	//FIXME:怎么处理
	if err != nil {
		log.Logger.Error("unable to remove macvlan key %v : %v", key, err)
		return err
	}
	return nil
}

func UpdateNetworkData(ip, network string, data string) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("updateNetworkData")
	_, err := etcdClient.etcdUpdate(key, data)
	if err != nil {
		return err
	}

	return nil
}

func CreateNetworkData(ip, network string, data string) error {
	key := networkDirNode + "/" + network + "/" + ip
	log.Logger.Debug("createNetworkData")
	_, err := etcdClient.etcdSet(key, data, nil)
	if err != nil {
		return err
	}

	return nil
}

func UpdateNodeContainerData(node string, data string) error {

	key := agentContainerNode + "/" + node
	log.Logger.Debug("UpdateNodeContainerData key:%v, data:%v", node, data)
	_, err := etcdClient.etcdSet(key, data, nil)
	if err != nil {
		log.Logger.Debug("UpdateNodeContainerData fail for :%v", err)
		return err
	}
	return nil
}

//空表示成功，否则为失败
func UpdateNetworkContainerResult(node string, data string) error {
	key := containerResultNode + "/" + node

	//设置TTL,自动删除结果节点.
	_, err := etcdClient.etcdSet(key, data, &client.SetOptions{TTL: ResultTTL})
	if err != nil {
		log.Logger.Debug("UpdateNetworkContainerResult fail for :%v", err)
		return err
	}
	return nil
}

//agent主机将自己的ip注册到etcd中
//问:etcd有没有机制检测多少时间内有更新节点?
//答:通过设置节点的TTL,agent必须在指定时间内更新/重新创建节点,一旦超时，节点丢失。 //可以认为该节点死亡.
//resp中有剩余的TTL时间
func RegisterNode(ip string) error {
	key := agentDirNode + "/" + ip
	_, err := etcdClient.etcdSet(key, ip, nil)
	if err != nil {
		log.Logger.Debug("Register Node fail for :%v", err)
		return err
	}
	return nil
}

func (e *EtcdClient) etcdSet(key string, content string, opt *client.SetOptions) (*client.Response, error) {
	//	log.Logger.Debug("set :key:%v,content:%v,option:%v", key, content, opt)

	resp, err := e.Set(context.Background(), key, content, opt)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (e *EtcdClient) etcdUpdate(key string, content string) (*client.Response, error) {
	resp, err := e.Update(context.Background(), key, content)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (e *EtcdClient) etcdGet(key string, opt *client.GetOptions) (*client.Response, error) {
	resp, err := e.Get(context.Background(), key, opt)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (e *EtcdClient) etcdDelete(key string, opt *client.DeleteOptions) (*client.Response, error) {
	resp, err := e.Delete(context.Background(), key, opt)
	if err != nil {
		return nil, err
	}
	return resp, nil

}
