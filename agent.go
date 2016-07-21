package main

import (
	"appnet-agent/daemon"
	"appnet-agent/etcd"
	"appnet-agent/log"
	"encoding/json"
	"os"
	"time"

	fsouza "github.com/fsouza/go-dockerclient"
)

var (
	DockerClient *daemon.DockerClient
	EtcdClient   *etcd.EtcdClient
	HostIP       string
	PoolManager  *VirNetworkPool
)

//1.监听本地docker事件,监听网络的变化
//2.根据etcd中macvlan节点的变更, 各主机节点进行macvlan网络的创建以及移除
//3.

//用户请求创建macvlan网络，appnet将macvlan信息记录到etcd中，agent主机一旦监听到该信息
//便创建macvlan网络

//谁来收集各appnet主机创建macvlan结果的信息?

//怎么知道有多少台agent主机？如果agent主机死掉了,appnet怎么知道?

//如果agent重启了，需要和etcd同步数据

//依赖于etcd进行数据同步..

//需要处理etcd的崩溃.

//两个节点，一个用来记录所有的agent节点，另一个记录创建按网络的结果.
// macvlan/nodes/ .. 区别所有的网络????所有通过ui创建macvlan网络具有同样的名字..
// macvlan/<macvlan name>/

//怎么创建macvlan创建网络的参数给agent?? ,创建节点时，赋值给key

//怎么通知appnet?

//预先写入某个节点，告知当前有多少台主机。 通过这个来对比.这里需要处理添加主机和删除主机，主机挂掉。

//在已经存在新的网络的情况下，新增加的主机需要创建之前的macvlan网络吗？如果创建失败了，应当如何去处理?
//删除指定的网络？还是告知有多少台主机加入了指定的网咯?
//暂时解决方法,假设添加主机必定能能够成功创建macvlan网络，如果失败，添加一条日志。到时候该主机添加网络失败
//再进行处理
func syncPool(etcdClient *etcd.EtcdClient, dockerClient *daemon.DockerClient, pool *VirNetworkPool, hostIP string) error {
	daemonNetwork, err := dockerClient.ListNetworks()
	if err != nil {
		log.Error("get daemon network info fail")
		return err
	}
	log.Debug("daemonNetwork:%v", daemonNetwork)

	//移除非macvlan的网络
	for k, v := range daemonNetwork {
		log.Debug("%v:%v", v.Name, v.Driver)
		if v.Driver != "macvlan" {
			daemonNetwork = append(daemonNetwork[:k], daemonNetwork[k+1:]...)
		}
	}

	//获取etcd中记录的该节点所有信息
	etcdNetwork, err := etcdClient.GetNetworks()
	if err != nil {
		log.Error("get etcd netwokr info fail")
		return err
	}

	//比较daemonNetwork和etcdNetwork, 找到存在etcd但不存在于daemon中的macvlan网络,忽略存在于daemon但不存在于etcd的macvlan
	//FIXME:这里最好坐下网络信息匹配，单凭名字来匹配会出现问题的。
	var inexistNetwork []string
	for _, j := range etcdNetwork {
		for _, v := range daemonNetwork {
			//已存在
			log.Debug("%v vs %v", j, v.Name)
			if v.Name == j {
				break
			}
			log.Debug("%v network doesn't exist in local daemon network")
			inexistNetwork = append(inexistNetwork, j)
		}
	}

	//XXX:更好的处理失败
	for _, j := range inexistNetwork {
		var param fsouza.CreateNetworkOptions

		//获取对应网络的macvlan网络创建参数
		//TODO:后续采用一个名字映射的方式?不一定使用同样的macvlan网络名?
		byteContent, err := etcdClient.GetNetworkParam(j)
		if err != nil {
			log.Error("unable to get network param:%v", err)
			continue
		}

		err = json.Unmarshal(byteContent, &param)
		if err != nil {
			log.Error("unable to json unmarshal:%v", err)
			continue
		}

		//创建缺失的网络
		newNetwork, err := dockerClient.CreateNetwork(param)
		if err != nil {
			log.Error("unable to create network :%v", err)
			continue
		}

		//获取详细信息
		fullNet, err := dockerClient.NetworkInfo(newNetwork.ID)
		if err != nil {
			log.Error("unable to inspect network :%v", err)
			//清理
			dockerClient.RemoveNetwork(fullNet.ID)
			continue
		}

		byteContent, err = json.Marshal(fullNet)
		if err != nil {
			log.Error("unable to marshal :%v", err)
			//清理
			dockerClient.RemoveNetwork(fullNet.ID)
			continue

		}

		//更新到etcd中
		err = etcdClient.UpdateNetworkData(hostIP, j, byteContent)
		if err != nil {
			log.Error("unable to update data to etcd :%v", err)
			//清理
			dockerClient.RemoveNetwork(fullNet.ID)
			continue
		}

		//更新到pool中
		virnet := VirNetwork{*fullNet}
		PoolManager.Lock()
		pool.Networks[j] = virnet
		PoolManager.Unlock()
	}

	return nil
}

func HandleEtcdNetworkEvent(eventChan <-chan etcd.EtcdNetworkEvent) {

	for {
		event := <-eventChan
		log.Debug("event:%v", event)
		//TODO:待完成
		//更新
		switch event.Action {
		case "create":
		case "delete":
		}

	}
}

func HandleDaemonNetworkEvent(eventChan <-chan daemon.DaemonNetworkEvent) {
	for {
		event := <-eventChan
		log.Debug("event:%v", event)

		//忽略非macvlan网络.
		//TODO:添加overlay网络的处理
		if event.Type != "macvlan" {
			continue
		}

		//忽略不是appnet创建的网络事件
		_, exists := PoolManager.Networks[event.Network]
		if !exists {
			continue
		}
		//TODO:待完成
		switch event.Action {
		case "create":
			//do nothing
		case "delete":
			//重新创建该网络
			var param fsouza.CreateNetworkOptions
			byteContent, err := EtcdClient.GetNetworkParam(event.Network)
			if err != nil {
				log.Error("unable to get network params:%v", err)
				continue
			}

			err = json.Unmarshal(byteContent, &param)
			if err != nil {
				log.Error("unable to unmarshal json: %v", err)
				continue
			}

			net, err := DockerClient.CreateNetwork(param)
			if err != nil {
				log.Error("unable to create network:%v", err)
				continue
			}

			fullnet, err := DockerClient.NetworkInfo(net.Name)
			if err != nil {
				log.Error("unable to inspect network:%v", err)
			}

			byteContent2, err := json.Marshal(net)
			if err != nil {
				log.Error("unable to json marshal:%v", err)
				continue
			}
			log.Error("Marshal data:%v", byteContent2)

			err = EtcdClient.UpdateNetworkData(HostIP, net.Name, byteContent2)
			if err != nil {
				log.Error("unable to update etcd data", err)
				//TODO:失败清理
			}

			PoolManager.Lock()
			virnet := VirNetwork{*fullnet}
			PoolManager.Networks[event.Network] = virnet
			PoolManager.Unlock()

		case "connect", "disconnect":
			//更新指定网络的信息
			//同步到etcd中
			net, err := DockerClient.NetworkInfo(event.Network)
			if err != nil {
				log.Error("unable to inspect network: %v", err)
				continue
			}

			byteContent, err := json.Marshal(net)
			if err != nil {
				log.Error("unable to json marshal:%v", err)
				continue
			}
			log.Error("Marshal data:%v", byteContent)

			err = EtcdClient.UpdateNetworkData(HostIP, net.Name, byteContent)
			if err != nil {
				log.Error("unable to update etcd data", err)
				//TODO:失败清理
			}

			PoolManager.Lock()
			virnet := VirNetwork{*net}
			PoolManager.Networks[event.Network] = virnet
			PoolManager.Unlock()
			//更新内存中的Pool
			//更新指定网络的信息
			//同步到etcd中
		}
	}
}

func main() {
	//TODO:配置参数
	//XXX:变更为全局?
	etcdClient := etcd.InitEtcdClient("192.168.4.11:2379")
	if etcdClient == nil {
		log.Error("init etcd fail")
		os.Exit(1)
	}
	EtcdClient = etcdClient

	dockerClient := daemon.InitDockerClient("unix:///var/run/docker.sock")
	if dockerClient == nil {
		log.Error("init daemon fail")
		os.Exit(1)
	}
	DockerClient = dockerClient

	//FIXME:需要更好的获取主机IP的方法
	nodeIp := dockerClient.GetNodeIP()
	if len(nodeIp) == 0 {
		log.Error("unabled to get current host ip")
		os.Exit(1)
	}

	go func() {
		for {
			err := etcdClient.RegisterNode(nodeIp)

			//如果etcd断开了，检测到etcd重新启动后，需要立即更新节点，避免主机认为节点已经断开连接
			if err != nil {
				if err.Error() == etcd.ErrClusterUnavailable.Error() {
					time.Sleep(1 * time.Second)
					continue
				}
			}

			time.Sleep(etcd.RegisterNodeTTL - 1*time.Second)
			log.Debug("update node per %v ", etcd.RegisterNodeTTL-1*time.Second)
		}
	}()

	//初始化内存macvlan网络拓扑
	//提取etcd中的数据
	PoolManager = InitPool()
	//如果etcd中的数据和docker daemon不同步
	//进行同步
	err := syncPool(etcdClient, dockerClient, PoolManager, HostIP)
	if err != nil {
		log.Error("sync pool fail:%v", err)
		os.Exit(1)
	}

	etcdNetworkChan := etcdClient.EtcdListenNetwork()
	go HandleEtcdNetworkEvent(etcdNetworkChan)

	daemonNetworkChan := daemon.DaemonListenNetwork()
	go HandleDaemonNetworkEvent(daemonNetworkChan)

	time.Sleep(110000 * time.Second)
}
