package controller

import (
	"bufio"
	"context"
	"fastgshare/fastfunc/internal/profiling"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/KontonGu/FaST-GShare/pkg/grpcclient"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	types "github.com/KontonGu/FaST-GShare/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// NodeStatus represents the readiness of a node.
type NodeStatus string

const (
	NodeReady    NodeStatus = "Ready"
	NodeNotReady NodeStatus = "NotReady"
)

// Node holds node info.
type Node struct {
	IP            string
	GrpcPort      int
	Status        NodeStatus
	GrpcClient    *grpcclient.GrpcClient
	LastHeartbeat time.Time
	NodeName      string

	availableGPUs []*seti.VirtualGPU //may change

	physicalGPUsMap map[string]*GPUInfo
	lock            sync.Mutex
}

// NodeManager manages node liveness for the autoscaler.
type NodeManager struct {
	nodes          map[string]*Node
	nodesMtx       sync.Mutex
	qpsStore       *profiling.QPSStore
	kubeClient     kubernetes.Interface
	checkTickerItv int
}

// NewNodeManager creates a new NodeManager.
func NewNodeManager(checkInterval int, qpsStore *profiling.QPSStore, kubeClient kubernetes.Interface) *NodeManager {

	interval := time.Duration(checkInterval) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	return &NodeManager{
		nodes:          make(map[string]*Node),
		checkTickerItv: int(interval.Seconds()),
		qpsStore:       qpsStore,
		kubeClient:     kubeClient,
	}
}

// UpdateHeartbeat updates the heartbeat timestamp for a node and marks it ready.
func (nm *NodeManager) UpdateHeartbeat(nodeName string) error {

	nm.nodesMtx.Lock()
	defer nm.nodesMtx.Unlock()

	if node, exists := nm.nodes[nodeName]; exists {
		node.Status = NodeReady
		node.LastHeartbeat = time.Now()
	} else {
		return fmt.Errorf("node %s not found", nodeName)
	}
	return nil
}

// CheckNodeLiveness checks all nodes and marks them NotReady if heartbeat is too old.
func (nm *NodeManager) CheckNodeLiveness() {
	nm.nodesMtx.Lock()
	defer nm.nodesMtx.Unlock()
	curTime := time.Now()
	for _, node := range nm.nodes {
		if curTime.Sub(node.LastHeartbeat).Seconds() > float64(nm.checkTickerItv) {
			node.Status = NodeNotReady
		}
	}
}

// GetNodeStatus returns the status of a node.
func (nm *NodeManager) GetNodeStatus(nodeName string) NodeStatus {
	nm.nodesMtx.Lock()
	defer nm.nodesMtx.Unlock()
	if node, exists := nm.nodes[nodeName]; exists {
		return node.Status
	}
	return NodeNotReady
}

// ListNodes returns a copy of the current node liveness map.
func (nm *NodeManager) ListNodes() map[string]Node {
	nm.nodesMtx.Lock()
	defer nm.nodesMtx.Unlock()
	copyMap := make(map[string]Node, len(nm.nodes))
	for k, v := range nm.nodes {
		copyMap[k] = *v
	}
	return copyMap
}

// StartTCPAcceptor starts a TCP server to accept node connections.
func (nm *NodeManager) StartTCPAcceptor(addr string, stopCh <-chan struct{}) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		klog.Errorf("Error while listening to the tcp socket %s: %v", addr, err)
		return err
	}
	defer listener.Close()

	klog.Infof("NodeManager listening for node connections on %s ...", addr)

	go func() {
		<-stopCh
		listener.Close()
		klog.Infof("NodeManager TCP acceptor stopped.")
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			klog.Errorf("Error while accepting the tcp socket: %v", err)
			continue
		}
		klog.Infof("Received connection from node: %s", conn.RemoteAddr().String())
		go nm.handleNodeConnection(conn)
	}
}

// handleNodeConnection handles a single node connection: handshake and heartbeat.
func (nm *NodeManager) handleNodeConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	nodeIP := strings.Split(conn.RemoteAddr().String(), ":")[0]

	// 1. Receive hello message
	res, err := reader.ReadBytes('\n')
	if err != nil {
		klog.Errorf("Error while reading hello message from node: %v", err)
		return
	}
	var helloMessage types.ConfiguratorNodeHelloMessage
	err = types.DecodeFromByte(res, &helloMessage)
	if err != nil {
		klog.Errorf("Error while decoding hello message from node: %v", err)
		return
	}
	nodeName := helloMessage.Hostname
	klog.Infof("Received hello from node %s (IP: %s, gRPC port: %d)", nodeName, nodeIP, helloMessage.GrpcPort)

	klog.Infof("Received hostname from node information: %s", helloMessage.Hostname)
	daemonPodName := nodeName
	daemonPod, err := nm.kubeClient.CoreV1().Pods("kube-system").Get(context.TODO(), daemonPodName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Error cannot find the node daemonset. Using the default node name.")
	}
	if len(daemonPod.Spec.NodeName) > 0 {

		nodeName = daemonPod.Spec.NodeName
		nodeIP = daemonPod.Status.HostIP
		klog.Infof("Node name is: %s", nodeName)
	}

	// 2. Send ack
	ackMsg := types.ConfiguratorNodeAckMessage{Ok: true}
	ackMsgBytes, err := types.EncodeToByte(ackMsg)
	if err != nil {
		klog.Errorf("Error encoding ack message: %v", err)
		return
	}
	_, err = conn.Write(ackMsgBytes)
	if err != nil {
		klog.Errorf("Error sending ack message: %v", err)
		return
	}

	//create grpc client
	client := grpcclient.NewGrpcClient()
	err = client.Connect(nodeIP, helloMessage.GrpcPort)
	if err != nil {
		klog.Errorf("Error while connecting to the node configurator.")
		klog.Errorf("Error: %v", err)
	}

	klog.Infof("Connected to the node configurator %s : %s:%d", nodeName, nodeIP, helloMessage.GrpcPort)

	health, err := client.Health(context.TODO())
	if err != nil {
		klog.Errorf("Error while checking the health of the node configurator.")
		klog.Errorf("Error: %v", err)
		return
	}
	if !health.Healthy {
		klog.Errorf("Node %s is not healthy (health says not healthy)", nodeName)
		return
	}

	response, err := client.GetAvailableGPUs(context.Background())
	if err != nil {
		klog.Errorf("Error: %v", err)
		klog.Errorf("Error while getting the available GPUs from the node configurator.")
		return
	}

	if response != nil && response.Gpus != nil {
		klog.Infof("Node %s has %d available GPUs", nodeName, len(response.Gpus))
	} else {
		klog.Infof("Node %s has no available GPUs", nodeName)
	}

	// Always create/replace node info after handshake

	//log if back online
	if node, ok := nm.nodes[nodeName]; ok {
		if node.Status == NodeNotReady {
			klog.Infof("Node %s is back online", nodeName)
		}
	} else {
		klog.Infof("New node %s is online", nodeName)
	}
	nm.nodesMtx.Lock()
	nm.nodes[nodeName] = &Node{
		IP:              nodeIP,
		GrpcPort:        helloMessage.GrpcPort,
		GrpcClient:      client,
		LastHeartbeat:   time.Now(),
		NodeName:        nodeName,
		Status:          NodeReady,
		physicalGPUsMap: make(map[string]*GPUInfo),
		availableGPUs:   response.Gpus,
	}
	nm.nodesMtx.Unlock()

	// 4. Heartbeat loop
	for {
		heartbeatMsg, err := reader.ReadBytes('\n')
		if err != nil {
			if err.Error() == "EOF" {
				klog.Infof("Node %s disconnected (EOF)", nodeName)
			} else {
				klog.Errorf("Error while reading heartbeat from node %s: %v", nodeName, err)
			}
			nm.nodesMtx.Lock()
			if node, ok := nm.nodes[nodeName]; ok {
				node.Status = NodeNotReady
			}
			nm.nodesMtx.Unlock()
			return
		}

		var heartBeat types.ConfiguratorHeartbeatMessage
		err = types.DecodeFromByte(heartbeatMsg, &heartBeat)
		if err != nil {
			klog.Errorf("Error decoding heartbeat from node %s: %v", nodeName, err)
			nm.nodesMtx.Lock()
			if node, ok := nm.nodes[nodeName]; ok {
				node.Status = NodeNotReady
			}
			nm.nodesMtx.Unlock()
			return
		}
		if !heartBeat.Alive {
			klog.Errorf("Node %s is not alive (heartbeat says not alive)", nodeName)
			nm.nodesMtx.Lock()
			if node, ok := nm.nodes[nodeName]; ok {
				node.Status = NodeNotReady
			}
			nm.nodesMtx.Unlock()
			return
		}
		nm.UpdateHeartbeat(nodeName)
		klog.Infof("Received heartbeat from node %s", nodeName)
	}
}
