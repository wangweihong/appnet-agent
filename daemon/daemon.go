package daemon

import (
	"appnet-agent/log"

	fsouza "github.com/fsouza/go-dockerclient"
)

var (
	client *DockerClient
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
func DaemonListenLocalEvent() <-chan interface{} {
	//daemonEventChan := make(chan DaemonNetworkEvent)
	daemonEventChan := make(chan interface{})

	go func(daemonEventChan chan interface{}) {
		eventChan := make(chan *fsouza.APIEvents)
		err := client.AddEventListener(eventChan)
		if err != nil {
			log.Logger.Error("fail to add listener")
			return
		}
		for {
			event := <-eventChan

			if event == nil {
				log.Logger.Error("recieve an nil event ?")
				continue
			}

			//忽略非网络事件
			switch event.Type {
			case "network":
				log.Logger.Debug("watch docker occur nework event")

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
				log.Logger.Debug("watch docker occur container event:%v", event)
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
func CreateNetwork(opt fsouza.CreateNetworkOptions) (*fsouza.Network, error) {
	network, err := client.CreateNetwork(opt)
	if err != nil {
		return nil, err
	}

	log.Logger.Debug("create %v", network)
	return network, nil
}

func InfoNetwork(NetworkId string) (*fsouza.Network, error) {
	network, err := client.NetworkInfo(NetworkId)
	if err != nil {
		log.Logger.Debug("info network fail :%v", err)
		return nil, err
	}
	return network, nil
}

//移除网络
func RemoveNetwork(id string) error {
	//检查关联的容器数量
	err := client.RemoveNetwork(id)
	if err != nil {
		log.Logger.Debug("remove %v fail:%v", id, err)
		return err
	}
	return nil
}

func ConnectToNetwork(nid, cid, ip string) error {
	var opt fsouza.NetworkConnectionOptions
	opt.Container = cid

	if len(ip) != 0 {
		config := new(fsouza.EndpointConfig)
		config.IPAMConfig = new(fsouza.EndpointIPAMConfig)
		config.IPAMConfig.IPv4Address = ip
		opt.EndpointConfig = config
		log.Logger.Debug("connectNetowrk:ip:%v", ip)
	}

	err := client.ConnectNetwork(nid, opt)
	if err != nil {
		log.Logger.Debug("ConnectNetwork [%v ===> %v]  fail for %v", cid, nid, err)
	}
	return err
}

func DisconnectFromNetwork(nid, cid string) error {
	var opt fsouza.NetworkConnectionOptions
	opt.Container = cid
	opt.Force = true
	err := client.DisconnectNetwork(nid, opt)
	if err != nil {
		log.Logger.Debug("DisconnectNetwork [%v ===> %v]  fail for %v", cid, nid, err)
	}
	return err
}

func ListNetworks() ([]fsouza.Network, error) {
	networks, err := client.ListNetworks()
	if err != nil {
		log.Logger.Debug("ListNetworks fail for %v\n", err)
		return nil, err
	}
	return networks, nil

}

//获取所有的所有的容器(id)
func GetAllContainers() ([]DaemonContainer, error) {
	apiContainers, err := client.ListContainers(fsouza.ListContainersOptions{})
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
	client = InitDockerClient(endpoint)
	if err != nil {
		panic("Create docker client fail")
	}
}
