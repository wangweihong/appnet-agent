package main

import (
	"appnet-agent/daemon"
	"appnet-agent/etcd"
	"appnet-agent/log"
	"os"
	"time"
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
func syncPool(etcdClient *etcd.EtcdClient, dockerClient *daemon.DockerClient, pool *VirNetworkPool) error {
	daemonNetwork, err := dockerClient.ListNetworks()
	if err != nil {
		log.Error("get daemon network info fail")
		return err
	}
	log.Debug("daemonNetwork:%v", daemonNetwork)

	//移除非macvlan的网络
	for k, v := range daemonNetwork {
		log.Debug("%v:%v", v.name, v.Driver)
		if v.Driver != "macvlan" {
			daemonNetwork = append(daemonNetwork[:k], daemonNetwork[k+1]...)
		}
	}

	//获取etcd中记录的该节点所有信息
	//这里要json转换
	etcdNetwork, err := etcdClient.GetNetworks()
	if err != nil {
		log.Error("get etcd netwokr info fail")
		return err
	}

	//比较daemonNetwork和etcdNetwork, 找到存在etcd但不存在于daemon中的macvlan网络,忽略存在于daemon但不存在于etcd的macvlan
	var inexistNetwork []string
	for _, j := range etcdNetwork {
		for _, v := range daemonNetwork {
			//已存在
			log.Debug("%v vs %v", j.Name, v.Name)
			if v.Name == j.Name {
				break
			}
			log.Debug("%v network doesn't exist in local daemon network")
			inexistNetwork = append(inexistNetwork, j.Name)
		}
	}

	//根据参数创建指定的网络
	//失败了怎么处理?
	for _, j := range inexistNetwork {

	}

	//获取对应网络的macvlan网络创建参数

	//创建缺失的网络

	//更新到etcd中

}

func main() {
	/*
		err := initLogger("./agent.log")
		if err != nil {
			panic(err)
		}
		NetworkListen()
	*/
	etcdClient := etcd.InitEtcdClient("192.168.4.11:2379")
	if client == nil {
		log.Error("init etcd fail")
		os.Exit(1)
	}

	dockerClient := daemon.InitDockerClient("unix:///var/run/docker.sock")
	if dockerClient == nil {
		log.Error("init daemon fail")
		os.Exit(1)
	}

	nodeIp := dockerClient.GetNodeIP()
	if len(nodeIp) == 0 {
		log.Error("unabled to get current host ip")
		os.Exit(1)
	}
	//初始化内存macvlan网络拓扑
	PoolManager := InitPool()

	//提取etcd中的数据

	//如果etcd中的数据和docker daemon不同步
	//进行同步

	//
	go func() {
		for {
			err := etcdClient.RegisterNode("192.168.14.14")

			//如果etcd断开了，检测到etcd重新启动后，需要立即更新节点，避免主机认为节点已经断开连接
			if err != nil {
				if err.Error() == etcd.ErrClusterUnavailable.Error() {
					time.Sleep(1 * time.Second)
					continue
				}
			}

			//			time.Sleep(etcd.RegisterNodeTTL - 1)
			time.Sleep(etcd.RegisterNodeTTL - 1*time.Second)
			log.Debug("update node per %v ", etcd.RegisterNodeTTL-1*time.Second)

		}

	}()

	time.Sleep(110000 * time.Second)

}
