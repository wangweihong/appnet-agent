package daemon

import (
	"appnet-agent/log"
	"strings"

	fsouza "github.com/fsouza/go-dockerclient"
)

var (
	Client *fsouza.Client

	//Channel用来接收etcd传递过来的事件?
)

type DockerClient struct {
	*fsouza.Client
}

func InitDockerClient(endpoint string) *DockerClient {
	var err error
	client, err := fsouza.NewClient(endpoint)
	if err != nil {
		log.Debug("create Docker client fail for: %v", err)
		return nil
	}

	return &DockerClient{client}
}

//要相应的更新etcd中数据才行
func NetworkListen() {
	eventChan := make(chan *fsouza.APIEvents)
	err := Client.AddEventListener(eventChan)
	if err != nil {
		log.Error("fail to add listener")
		return
	}
	for {
		event := <-eventChan

		log.Debug("Action:%v\n", event.Action)
		log.Debug("Type:%v\n", event.Type)
		log.Debug("Actor:%v __ %v\n", event.Actor.ID, event.Actor.Attributes)
		attrs := event.Actor.Attributes
		network, _ := attrs["name"]
		netType, _ := attrs["type"]
		containerEndpoin, _ := attrs["container"]
		//only concern about network type event
		if event.Type == "network" {
			switch event.Action {
			case "create":
				if len(network) == 0 || len(netType) == 0 {
					log.Error("daemon event attributes has changed")
					continue
				}

			case "destroy":
				if len(network) == 0 || len(netType) == 0 {
					log.Error("daemon event attributes has changed")
					continue
				}
			case "connect":
				if len(network) == 0 || len(netType) == 0 || len(containerEndpoin) == 0 {
					log.Error("daemon event attributes has changed")
					continue
				}
			case "disconnect":
				if len(network) == 0 || len(netType) == 0 || len(containerEndpoin) == 0 {
					log.Error("daemon event attributes has changed")
					continue
				}
			}
		}
	}
}

//创建docker network
//怎么返回结果？同步到etcd?
//关键是返回失败的结果.
func NetworkCreate(opt fsouza.CreateNetworkOptions) error {
	network, err := Client.CreateNetwork(opt)
	if err != nil {
		return err
	}

	log.Debug("%v", network)
	return nil
}

func NetworkRemove(id string) {
	//检查关联的容器数量
	Client.RemoveNetwork(id)
}

//获得当前主机节点的IP地址
func (c *DockerClient) GetNodeIP() string {
	dockerInfo, err := c.Info()
	if err != nil {
		log.Error("unable to get docker info: %v", err)
		return ""
	}

	advertise := dockerInfo.ClusterAdvertise
	slice := strings.Split(advertise, ":")
	if len(slice) != 2 {
		log.Error("ClusterAdvertise info has change")
		return ""
	}

	return slice[0]
}

func init() {
	endpoint := "unix:///var/run/docker.sock"
	var err error
	Client, err = InitDockerClient(endpoint)
	if err != nil {
		panic("Create docker client fail")
	}
}
