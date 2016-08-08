package daemon

import (
	"appnet-agent/log"

	fsouza "github.com/fsouza/go-dockerclient"
)

var (
	Client *DockerClient

	//Channel用来接收etcd传递过来的事件?
)

type DockerClient struct {
	*fsouza.Client
}

type DaemonNetworkEvent struct {
	Action   string //网络事件的动作
	Network  string //这个是ID
	Type     string
	Endpoint string //只有动作为connect和disconnect时有效
}

type DaemonContainerEvent struct {
	Action string
	Id     string //
}

type DaemonContainer struct {
	Id string `json:"id"`
}

func InitDockerClient(endpoint string) *DockerClient {
	var err error
	client, err := fsouza.NewClient(endpoint)
	if err != nil {
		log.Logger.Debug("create Docker client fail for: %v", err)
		return nil
	}

	return &DockerClient{client}
}

//要相应的更新etcd中数据才行
func DaemonListenNetwork() <-chan interface{} {
	//daemonEventChan := make(chan DaemonNetworkEvent)
	daemonEventChan := make(chan interface{})

	go func(daemonEventChan chan interface{}) {
		eventChan := make(chan *fsouza.APIEvents)
		err := Client.AddEventListener(eventChan)
		if err != nil {
			log.Logger.Error("fail to add listener")
			return
		}
		for {
			event := <-eventChan

			//忽略非网络事件
			switch event.Type {
			case "network":

				//only concern about network type event
				attrs := event.Actor.Attributes
				network, _ := attrs["name"]
				netType, _ := attrs["type"]

				if netType == "macvlan" {

					log.Logger.Debug("Action:%v", event.Action)
					log.Logger.Debug("Type:%v", event.Type)
					log.Logger.Debug("Actor:%v __ %v", event.Actor.ID, event.Actor.Attributes)
					containerEndpoint, _ := attrs["container"]
					networkEvent := DaemonNetworkEvent{
						Action:   event.Action,
						Network:  network,
						Type:     netType,
						Endpoint: containerEndpoint,
					}

					if len(network) == 0 || len(netType) == 0 {
						log.Logger.Warn("daemon event attributes has changed")
					}
					if event.Action == "disconnect" || event.Action == "connect" {
						if len(containerEndpoint) == 0 {
							log.Logger.Warn("daemon event attributes has changed")
						}
					}
					daemonEventChan <- networkEvent

				}
			case "container":
				containerID := event.Actor.ID

				containerEvent := DaemonContainerEvent{
					Action: event.Action,
					Id:     containerID,
				}
				daemonEventChan <- containerEvent

			}
		}
	}(daemonEventChan)

	return daemonEventChan
}

//创建docker network
//怎么返回结果？同步到etcd?
//关键是返回失败的结果.
func NetworkCreate(opt fsouza.CreateNetworkOptions) error {
	network, err := Client.CreateNetwork(opt)
	if err != nil {
		return err
	}

	log.Logger.Debug("%v", network)
	return nil
}

func NetworkRemove(id string) {
	//检查关联的容器数量
	Client.RemoveNetwork(id)
}

func (c *DockerClient) ConnectToNetwork(nid, cid string) error {
	var opt fsouza.NetworkConnectionOptions
	opt.Container = cid
	err := c.ConnectNetwork(nid, opt)
	if err != nil {
		log.Logger.Debug("ConnectNetwork [%v ===> %v]  fail for %v", cid, nid, err)
	}
	return err
}

func (c *DockerClient) DisconnectFromNetwork(nid, cid string) error {
	var opt fsouza.NetworkConnectionOptions
	opt.Container = cid
	opt.Force = true
	err := c.DisconnectNetwork(nid, opt)
	if err != nil {
		log.Logger.Debug("DisconnectNetwork [%v ===> %v]  fail for %v", cid, nid, err)
	}
	return err
}

//获取所有的所有的容器(id)
func (c *DockerClient) GetAllContainers() ([]DaemonContainer, error) {
	apiContainers, err := c.ListContainers(fsouza.ListContainersOptions{})
	if err != nil {
		return []DaemonContainer{}, err
	}

	containers := make([]DaemonContainer, 0)
	for _, v := range apiContainers {
		var container DaemonContainer
		container.Id = v.ID
		containers = append(containers, container)
	}
	return containers, nil
}

func init() {
	endpoint := "unix:///var/run/docker.sock"
	var err error
	Client = InitDockerClient(endpoint)
	if err != nil {
		panic("Create docker client fail")
	}
}
