package main

/*

 agent程序在运行后，将会进行以下的操作:
 1.连接到etcd server中，将在/macvlan/nodes/目录节点下创建一个名为为当前agent IP的节点,
 该节点的TTL时长为5s, 每隔4s会进行一次更新操作。超时后，该节点将会丢失，视为当前agent断开连接.

 2.获取/macvlan/network/params目录下的所有节点，提取其中的网络创建参数，对比agent本地docker上的macvlan网络，创建不存在本地的macvlan网络
   创建成功后将详细的网络信息保存在/macvlan/network/nodes/<network>/<agentIP>节点中

 3.监听本地docker daemon,如果记录到etcd中的网络被从命令行移除，则重新创建(?)
	如果有执行添加/移除容器出网络时，更新详细的信息到/macvlan/network/nodes/<network>/<agentIP>

*/

import (
	"appnet-agent/daemon"
	"appnet-agent/etcd"
	"appnet-agent/log"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/coreos/etcd/client"
	fsouza "github.com/fsouza/go-dockerclient"
)

var (
	DockerClient *daemon.DockerClient
	EtcdClient   *etcd.EtcdClient
	HostIP       string
	PoolManager  *VirNetworkPool
)

func syncPool(etcdClient *etcd.EtcdClient, dockerClient *daemon.DockerClient, pool *VirNetworkPool, hostIP string) error {
	daemonNetwork, err := dockerClient.ListNetworks()
	if err != nil {
		log.Logger.Error("get daemon network info fail")
		return err
	}

	//移除非macvlan的网络
	var daemonMaclvanNetwork []fsouza.Network
	for _, v := range daemonNetwork {
		if v.Driver == "macvlan" {
			daemonMaclvanNetwork = append(daemonMaclvanNetwork, v)
		}
	}
	log.Logger.Debug("macvlan networ in daemon: %v", daemonMaclvanNetwork)

	//获取etcd中记录的该节点所有信息
	etcdNetwork, err := etcdClient.GetAllNetworkCreateParams()
	if err != nil {
		log.Logger.Error("get all etcd network params fail: %v", err)
		return err
	}
	log.Logger.Debug("all etcd param:%v", etcdNetwork)

	//获取所有网络的创建参数
	etcdNetworkParams := make([]fsouza.CreateNetworkOptions, 0)

	for _, v := range etcdNetwork {
		var param fsouza.CreateNetworkOptions
		err := json.Unmarshal([]byte(v), &param)
		if err != nil {
			log.Logger.Error("json unmarshal network param fail:%v", err)
			return err
		}

		etcdNetworkParams = append(etcdNetworkParams, param)
	}

	//比较daemonNetwork和etcdNetwork, 找到存在etcd但不存在于daemon中的macvlan网络,忽略存在于daemon但不存在于etcd的macvlan
	var inexistNetwork []fsouza.CreateNetworkOptions
	//记录:详细网络信息
	FullNetInfos := make([]fsouza.Network, 0)
	for _, j := range etcdNetworkParams {
		var isFound bool
		var foundNetId string
		for _, v := range daemonMaclvanNetwork {
			//已存在
			//TODO:1
			log.Logger.Debug("%v vs %v", j.Name, v.Name)
			//1.比较名字
			if v.Name != j.Name {
				goto Done
			}
			//2:比较macvlan option
			for m, n := range j.Options {
				if opt, exists := v.Options[m]; !exists || opt != n {
					goto Done
				}
			}

			//3.比较subnet和ipam驱动
			if j.IPAM.Config[0].Subnet != v.IPAM.Config[0].Subnet || j.IPAM.Driver != v.IPAM.Driver {
				goto Done
			}
			isFound = true
			foundNetId = v.ID

			goto Done
		}
	Done:
		if !isFound {
			log.Logger.Debug("%v network doesn't exist in local daemon network", j.Name)
			inexistNetwork = append(inexistNetwork, j)
		} else {
			//提交已有的网络信息

			//获取详细信息
			fullNet, err := dockerClient.NetworkInfo(foundNetId)
			if err != nil {
				log.Logger.Error("unable to inspect network :%v", err)
				//清理
				continue
			}
			FullNetInfos = append(FullNetInfos, *fullNet)
		}
	}

	//XXX:更好的处理失败
	log.Logger.Debug("inexistNetwork : %v", inexistNetwork)
	for _, j := range inexistNetwork {
		//创建缺失的网络
		newNetwork, err := dockerClient.CreateNetwork(j)
		if err != nil {
			log.Logger.Error("unable to create network :%v", err)
			continue
		}
		//获取详细信息
		fullNet, err := dockerClient.NetworkInfo(newNetwork.ID)
		if err != nil {
			log.Logger.Error("unable to inspect network :%v", err)
			//清理
			dockerClient.RemoveNetwork(fullNet.ID)
			continue
		}
		FullNetInfos = append(FullNetInfos, *fullNet)

	}

	for _, v := range FullNetInfos {

		byteContent, err := json.Marshal(v)
		if err != nil {
			log.Logger.Error("unable to marshal :%v", err)
			//清理
			dockerClient.RemoveNetwork(v.ID)
			continue
		}

		//更新到etcd中
		err = etcdClient.CreateNetworkData(hostIP, v.Name, byteContent)
		if err != nil {
			log.Logger.Error("unable to update data to etcd :%v", err)
			//清理
			dockerClient.RemoveNetwork(v.ID)
			continue
		}
	}

	return nil
}

func HandleEtcdNetworkEvent(eventChan <-chan etcd.EtcdNetworkEvent) {

	for {
		event := <-eventChan
		log.Logger.Debug("event:%v", event)
		//TODO:待完成
		//更新
		switch event.Action {
		case "create":
			//如何获取新建节点的名字
			var network string
			var param fsouza.CreateNetworkOptions
			byteContent, err := EtcdClient.GetNetworkParam(network)
			if err != nil {
				//TODO:更好的失败处理方法
				log.Logger.Error("unable to get Network Param :%v", err)
				continue
			}

			err = json.Unmarshal(byteContent, &param)
			if err != nil {
				log.Logger.Error("unable to unmarshal json: %v", err)
				continue
			}

			net, err := DockerClient.CreateNetwork(param)
			if err != nil {
				log.Logger.Error("unable to create network:%v", err)
				continue
			}

			fullnet, err := DockerClient.NetworkInfo(net.Name)
			if err != nil {
				log.Logger.Error("unable to inspect network:%v", err)
			}

			byteContent2, err := json.Marshal(net)
			if err != nil {
				log.Logger.Error("unable to json marshal:%v", err)
				continue
			}
			log.Logger.Error("Marshal data:%v", byteContent2)

			err = EtcdClient.CreateNetworkData(HostIP, net.Name, byteContent2)
			if err != nil {
				log.Logger.Error("unable to update etcd data", err)
				//TODO:失败清理
			}

			PoolManager.Lock()
			virnet := VirNetwork{*fullnet}
			PoolManager.Networks[param.Name] = virnet
			PoolManager.Unlock()

			/*
				case "delete":
					//TODO: how to get network id and networkID
					//placeholder
					var netID string
					var network string
					err := DockerClient.RemoveNetwork(netID)
					if err != nil {
						log.Logger.Error("unable to remove network %v : %v", netID, err)
					}

					PoolManager.Lock()
					delete(PoolManager.Networks, network)
					PoolManager.Unlock()
			*/
		}
	}
}

func HandleDaemonNetworkEvent(eventChan <-chan interface{}) {
	for {
		e := <-eventChan
		switch event := e.(type) {
		case daemon.DaemonNetworkEvent:
			log.Logger.Debug("event:%v", event)

			//忽略非macvlan网络.
			//TODO:添加overlay网络的处理
			if event.Type != "macvlan" {
				continue
			}

			log.Logger.Debug("get macvlan network event:%v", event)
			//忽略不是appnet创建的网络事件

			switch event.Action {
			case "create":
				//do nothing
			case "destroy":
				//重新创建该网络
				var param fsouza.CreateNetworkOptions
				byteContent, err := EtcdClient.GetNetworkParam(event.Network)
				if err != nil {
					e := err.(client.Error)
					//这是由appnet发出删除网络的操作
					if e.Code == client.ErrorCodeKeyNotFound {
						continue
					} else {
						log.Logger.Critical("GetNetworkParam fail：%v", err)
						continue
					}
				}

				err = json.Unmarshal(byteContent, &param)
				if err != nil {
					log.Logger.Error("unable to unmarshal json: %v", err)
					continue
				}

				net, err := DockerClient.CreateNetwork(param)
				if err != nil {
					log.Logger.Error("unable to create network:%v", err)
					continue
				}

				fullnet, err := DockerClient.NetworkInfo(net.Name)
				if err != nil {
					log.Logger.Error("unable to inspect network:%v", err)
				}

				byteContent2, err := json.Marshal(net)
				if err != nil {
					log.Logger.Error("unable to json marshal:%v", err)
					continue
				}
				log.Logger.Debug("Marshal data:%v", string(byteContent2))

				err = EtcdClient.CreateNetworkData(HostIP, net.Name, byteContent2)
				if err != nil {
					log.Logger.Error("unable to update etcd data", err)
					//TODO:失败清理
				}

				PoolManager.Lock()
				virnet := VirNetwork{*fullnet}
				PoolManager.Networks[event.Network] = virnet
				PoolManager.Unlock()

			case "connect", "disconnect":
				//更新指定网络的信息
				//同步到etcd中
				//FIXME:我们需要将完整的信息保存在etcd中吗?需要,appnet需要手机信息
				net, err := DockerClient.NetworkInfo(event.Network)
				if err != nil {
					log.Logger.Error("unable to inspect network: %v", err)
					continue
				}

				byteContent, err := json.Marshal(net)
				if err != nil {
					log.Logger.Error("unable to json marshal:%v", err)
					continue
				}
				log.Logger.Debug("Marshal data:%v", byteContent)

				err = EtcdClient.UpdateNetworkData(HostIP, net.Name, byteContent)
				if err != nil {
					log.Logger.Error("unable to update etcd data", err)
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
			//每次ddamon创建容器/删除容器时，更新映射
		case daemon.DaemonContainerEvent:
			switch event.Action {
			case "create", "die":
				//更新节点上的数据
				//重新获取所有网络的数据
				containers, err := DockerClient.GetAllContainers()
				if err != nil {
					//TODO:失败处理
					log.Logger.Error("Get all containers fail %v", err)
					continue
				}

				byteContent, err := json.Marshal(containers)
				if err != nil {
					log.Logger.Error("json Unmarshal fail %v", err)
					continue
				}

				err := EtcdClient.UpdateNodeContainerData(HostIP, byteContent)
				if err != nil {
					log.Logger.Error("UpdateNodeContainerData:%v", err)
					continue
				}
			}
		}
	}
}

func checkNetworkParamExists(network string) bool {
	_, err := EtcdClient.GetNetworkParam(network)
	if err != nil {
		e := err.(client.Error)
		if e.Code == client.ErrorCodeKeyNotFound {
			return false
		}
		log.Logger.Debug("checkParam:%v", err)
	}

	return true
}

func HandleEtcdNetworkParamEvent(eventChan <-chan etcd.EtcdNetworkParamEvent) {
	for {
		event := <-eventChan
		log.Logger.Debug("network param event:%v", event)

		//在agent仍然在创建主机时，由于某台agent主机失败，导致appnet决定停止该网络的创建
		//需要机制来通知正在创建工作的停止
		//stageChan := make(chan string)

		//忽略非macvlan网络.
		//TODO:添加overlay网络的处理
		switch event.Action {

		//TODO:当set动作还没有完成时，appnet又传递了删除网络的动作..
		//使用goroutine來异步实现
		case "set":
			go func(event etcd.EtcdNetworkParamEvent, HostIP string) {
				networkKey := event.Node.Key
				paramByteContent := event.Node.Value
				networkName := filepath.Base(networkKey)
				if len(paramByteContent) == 0 {
					log.Logger.Debug("invalid network create param")
					return
				}
				log.Logger.Debug("EventNetworkParam network:%v param:%v", networkKey, paramByteContent)

				//appnet删除了网络参数
				if !checkNetworkParamExists(networkName) {
					log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
					return
				}

				var opt fsouza.CreateNetworkOptions
				err := json.Unmarshal([]byte(paramByteContent), &opt)
				if err != nil {
					log.Logger.Error("unable to unmarshal %v : %v ", paramByteContent, err)
					//TODO: 这里要在etcd中添加一个network节点，其内容为空，表示有agent创建macvlan网络失败
					EtcdClient.CreateNetworkData(HostIP, filepath.Base(networkKey), []byte{})
					return
				}

				//appnet删除了网络参数
				if !checkNetworkParamExists(networkName) {
					log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
					return
				}

				log.Logger.Debug("local docker daemon start to create macvlan network")
				network, err := DockerClient.CreateNetwork(opt)
				if err != nil {
					//TODO: 这里要在etcd中添加一个network节点，其内容为空，表示有agent创建macvlan网络失败
					log.Logger.Debug("create Network fail,update network key(%v:%v) : %v", HostIP, opt.Name, err)
					//appnet删除了网络参数
					if !checkNetworkParamExists(networkName) {
						log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
						return
					}
					EtcdClient.CreateNetworkData(HostIP, opt.Name, []byte{})
					return
				}

				//appnet删除了网络参数
				if !checkNetworkParamExists(networkName) {
					log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
					err := DockerClient.RemoveNetwork(network.ID)
					if err != nil {
						log.Logger.Debug("remove network fail after recieve fail create notification: %v", err)
					}
					return
				}
				log.Logger.Debug("start to get full network(%v) info", network.Name)
				allNetworkInfo, err := DockerClient.NetworkInfo(network.ID)
				if err != nil {
					//TODO: 这里要在etcd中添加一个network节点，其内容为空，表示有agent创建macvlan网络失败
					DockerClient.RemoveNetwork(network.ID)
					log.Logger.Debug("Network Info fail,update network key(%v:%v) : %v", HostIP, network.Name, err)
					//appnet删除了网络参数
					if !checkNetworkParamExists(networkName) {
						log.Logger.Debug("param %v has been removed. skip creating stage,", networkName)
						return
					}
					EtcdClient.CreateNetworkData(HostIP, network.Name, []byte{})
					return
				}

				//appnet删除了网络参数
				if !checkNetworkParamExists(networkName) {
					log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
					err := DockerClient.RemoveNetwork(network.ID)
					if err != nil {
						log.Logger.Debug("remove network fail after recieve fail create notification: %v", err)
					}
					return
				}
				//writeToEtcd..
				log.Logger.Debug("marshal network(%v) info", network.Name)
				byteContent, err := json.Marshal(allNetworkInfo)
				if err != nil {
					//TODO: 这里要在etcd中添加一个network节点，其内容为空，表示有agent创建macvlan网络失败
					log.Logger.Debug("json marshal fail,update network key:(%v:%v) : %v", HostIP, network.Name, err)
					DockerClient.RemoveNetwork(network.ID)
					//appnet删除了网络参数
					if !checkNetworkParamExists(networkName) {
						log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
						return
					}
					EtcdClient.CreateNetworkData(HostIP, network.Name, []byte{})
					return
				}

				//
				//appnet删除了网络参数
				if !checkNetworkParamExists(networkName) {
					log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
					err := DockerClient.RemoveNetwork(network.ID)
					if err != nil {
						log.Logger.Debug("remove network fail after recieve fail create notification: %v", err)
					}
					return
				}

				log.Logger.Debug("start to update network(%v) data", network.Name)
				err = EtcdClient.CreateNetworkData(HostIP, network.Name, byteContent)
				if err != nil {
					//TODO:网络清理...
					log.Logger.Error("update network data fail (%v:%v) : %v", HostIP, network.Name, err)
					DockerClient.RemoveNetwork(network.ID)

					//appnet删除了网络参数
					if !checkNetworkParamExists(networkName) {
						log.Logger.Debug("param %v has been removed. skip creating stage", networkName)
						return
					}
					//TODO: 这里要在etcd中添加一个network节点，其内容为空，表示有agent创建macvlan网络失败
					EtcdClient.CreateNetworkData(HostIP, network.Name, []byte{})
					return
				}

			}(event, HostIP)
		case "delete":
			//TODO:提取指定的网络参数相应的key,对应的网络集群名
			//在networkNdirNode中找到该网络集群中，当前agent对应的真正网络信息
			//加以删除
			go func(event etcd.EtcdNetworkParamEvent, HostIP string) {
				networkKey := event.Node.Key
				paramByteContent := event.Node.Value
				log.Logger.Debug("delete====> EventNetworkParam network:%v param:%v", networkKey, paramByteContent)
				networkName := filepath.Base(networkKey)
				log.Logger.Debug("delete====> networkName:%v:HostIP:%v", networkName, HostIP)
				localNetwork, err := EtcdClient.InfoNetwork(HostIP, networkName)
				if err != nil {
					log.Logger.Debug("delete====> get real local network info fail : %v", err)
					return
				}

				if localNetwork == nil {
					log.Logger.Debug("delete===> agent didn't create %v network", networkName)
					return
				}
				log.Logger.Debug("delete===> real local network :%v", localNetwork)
				//先删除网络节点
				err = EtcdClient.RemoveNetworkData(HostIP, networkName)
				if err != nil {
					log.Logger.Debug("delete===> remove network data fail:%v", err)
					return
				}

				//再删除网络数据
				err = DockerClient.RemoveNetwork(localNetwork.ID)
				if err != nil {
					log.Logger.Debug("delete====> remove network fail:%v", err)
					return
				}
			}(event, HostIP)
		}
	}
}

func main() {
	//TODO:配置参数
	//XXX:变更为全局?
	var etcdEndpoint string

	flag.StringVar(&etcdEndpoint, "endpoint", "192.168.4.11:2379", "ip of etcd server")
	flag.Parse()

	if len(etcdEndpoint) == 0 {
		log.Logger.Error("etcd endpoint is invalid")
		os.Exit(1)
	}

	//HostIP = "192.168.4.11"
	HostIP = os.Getenv("CATTLE_AGENT_IP")
	if len(HostIP) == 0 {
		log.Logger.Error("can't not get agent IP")
		os.Exit(1)
	}

	etcdClient := etcd.InitEtcdClient(etcdEndpoint)
	if etcdClient == nil {
		log.Logger.Error("init etcd fail")
		os.Exit(1)
	}
	EtcdClient = etcdClient

	dockerClient := daemon.InitDockerClient("unix:///var/run/docker.sock")
	if dockerClient == nil {
		log.Logger.Error("init daemon fail")
		os.Exit(1)
	}
	DockerClient = dockerClient

	go func() {
		for {
			err := etcdClient.RegisterNode(HostIP)

			//如果etcd断开了，检测到etcd重新启动后，需要立即更新节点，避免主机认为节点已经断开连接
			if err != nil {
				if err.Error() == etcd.ErrClusterUnavailable.Error() {
					time.Sleep(1 * time.Second)
					continue
				}
			}

			time.Sleep(etcd.RegisterNodeTTL - 1*time.Second)
			//log.Logger.Debug("update node per %v ", etcd.RegisterNodeTTL-1*time.Second)
		}
	}()

	//初始化内存macvlan网络拓扑
	//提取etcd中的数据
	PoolManager = InitPool()
	//如果etcd中的数据和docker daemon不同步
	//进行同步
	err := syncPool(etcdClient, dockerClient, PoolManager, HostIP)
	if err != nil {
		log.Logger.Error("sync pool fail:%v", err)
		os.Exit(1)
	}

	etcdNetworkChan := etcdClient.EtcdListenNetwork()
	go HandleEtcdNetworkEvent(etcdNetworkChan)

	daemonEventChan := daemon.DaemonListenNetwork()
	go HandleDaemonNetworkEvent(daemonEventChan)

	etcdNetworkParamChan := etcdClient.EtcdListenNetworkParam()
	go HandleEtcdNetworkParamEvent(etcdNetworkParamChan)

	doneChan := make(chan bool)
	_ = <-doneChan
}
