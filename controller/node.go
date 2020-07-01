package controller

import (
	"context"
	"log"
	"path"
	"runtime"

	"github.com/pbnjay/memory"

	// "k8s.io/client-go/tools/clientcmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	// coordv1 "k8s.io/api/coordination/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	// corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	"k8s.io/apimachinery/pkg/fields"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	// "github.com/virtual-kubelet/virtual-kubelet/log"

	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/danopia/kube-edge-node/podman"
)

// type NP struct {}
// func (np *NP) NotifyNodeStatus(	)

type EdgeNode struct {
	NodeName          string
	Podman            *podman.PodmanClient
	Kubernetes        *kubernetes.Clientset
	NodeRunner        *node.NodeController
	PodRunner         *node.PodController
	PodInformer       corev1informers.PodInformer
	SecretInformer    corev1informers.SecretInformer
	ConfigMapInformer corev1informers.ConfigMapInformer
	ServiceInformer   corev1informers.ServiceInformer
}

func NewEdgeNode(nodeName string, podman *podman.PodmanClient, kubernetes *kubernetes.Clientset) (*EdgeNode, error) {

	localImages, err := podman.List(context.TODO())
	if err != nil {
		return nil, err
	}
	var localImagesMapped []corev1.ContainerImage
	for _, img := range localImages {
		localImagesMapped = append(localImagesMapped, corev1.ContainerImage{
			Names:     img.Names,
			SizeBytes: img.Size,
		})
	}

	conVersion, err := podman.Version(context.TODO())
	if err != nil {
		return nil, err
	}

	pNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"purpose": "edge",
			},
		},
		Spec: corev1.NodeSpec{
			PodCIDR:    "10.6.2.33/27",
			PodCIDRs:   []string{"10.6.2.33/27"},
			ProviderID: "metal://" + nodeName,
			Taints: []corev1.Taint{{
				Key:    "kubernetes.io/edge-node",
				Value:  "edge",
				Effect: "NoSchedule",
			}},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				"cpu":               *resource.NewScaledQuantity(int64(runtime.NumCPU()), 0),
				"memory":            *resource.NewQuantity(int64(memory.TotalMemory()), resource.BinarySI),
				"pods":              resource.MustParse("1"), // TODO
				"ephemeral-storage": resource.MustParse("0"), // TODO
				"hugepages-2Mi":     resource.MustParse("0"),
			},
			Allocatable: corev1.ResourceList{
				"cpu":               resource.MustParse("1000m"),
				"memory":            resource.MustParse("1000Mi"),
				"pods":              resource.MustParse("1"),
				"ephemeral-storage": resource.MustParse("0"),
				"hugepages-2Mi":     resource.MustParse("0"),
			},
			Conditions: []corev1.NodeCondition{
				{
					// lastHeartbeatTime: "2020-06-30T17:20:59Z"
					// lastTransitionTime: "2020-05-18T22:36:38Z"
					Message: "Hello World",
					Reason:  "KubeletReady",
					Status:  "True",
					Type:    "Ready",
				},
				{
					Message: "Hello World",
					Reason:  "OK",
					Status:  "False",
					Type:    "MemoryPressure",
				},
				{
					Message: "Hello World",
					Reason:  "OK",
					Status:  "False",
					Type:    "DiskPressure",
				},
				{
					Message: "Hello World",
					Reason:  "OK",
					Status:  "False",
					Type:    "PIDPressure",
				},
			},
			Images: localImagesMapped,
			NodeInfo: corev1.NodeSystemInfo{
				Architecture:            conVersion.Arch,
				MachineID:               nodeName, // TODO: /etc/machine-id
				KernelVersion:           conVersion.KernelVersion,
				OSImage:                 "Debian GNU/Linux 10 (buster)", // TODO: /etc/os-release
				ContainerRuntimeVersion: "podman://" + conVersion.Version,
				KubeletVersion:          "metal/v0.1.0", // TODO?
				OperatingSystem:         conVersion.Os,
			},
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeHostName,
					Address: nodeName,
				},
				{
					Type:    corev1.NodeInternalIP,
					Address: "10.6.24.27",
				},
				{
					Type:    corev1.NodeInternalDNS,
					Address: nodeName + ".local",
				},
				{
					Type:    corev1.NodeExternalIP,
					Address: "35.222.199.140",
				},
			},
		},
		// status:
		//   daemonEndpoints:
		//     kubeletEndpoint:
		//       Port: 10250
		//   volumesAttached: []
		//   volumesInUse: []
	}

	nodeRunner, err := node.NewNodeController(node.NaiveNodeProvider{}, pNode, kubernetes.CoreV1().Nodes(), node.WithNodeEnableLeaseV1Beta1(kubernetes.CoordinationV1beta1().Leases(corev1.NamespaceNodeLease), nil))
	if err != nil {
		return nil, err
	}

	//https://github.com/virtual-kubelet/virtual-kubelet/blob/3ec3b14e49d0c2f335ca049155d1ee94b2baf35f/cmd/virtual-kubelet/internal/commands/root/root.go

	// Create a shared informer factory for Kubernetes pods in the current namespace (if specified) and scheduled to the current node.
	podInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubernetes,
		60*1000000000,
		// kubeinformers.WithNamespace(c.KubeNamespace),
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", pNode.Name).String()
		}))
	podInformer := podInformerFactory.Core().V1().Pods()

	// Create another shared informer factory for Kubernetes secrets and configmaps (not subject to any selectors).
	scmInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubernetes, 60*1000000000)
	// Create a secret informer and a config map informer so we can pass their listers to the resource manager.
	secretInformer := scmInformerFactory.Core().V1().Secrets()
	configMapInformer := scmInformerFactory.Core().V1().ConfigMaps()
	serviceInformer := scmInformerFactory.Core().V1().Services()

	eb := record.NewBroadcaster()
	eb.StartLogging(func(a string, b ...interface{}) {
		log.Printf("record: %+v %+v", a, b)
	})
	// eb.StartLogging(log.G(context.TODO()).Infof)
	eb.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: kubernetes.CoreV1().Events("kube-system")})

	// setup other things
	podRunner, err := node.NewPodController(node.PodControllerConfig{
		PodClient: kubernetes.CoreV1(),
		Provider: &PodmanProvider{
			podman:      podman,
			pods:        make(map[string]*corev1.Pod),
			podNotifier: func(*corev1.Pod) {},
		},

		PodInformer:       podInformer,
		EventRecorder:     eb.NewRecorder(scheme.Scheme, corev1.EventSource{Component: path.Join(pNode.Name, "pod-controller")}),
		SecretInformer:    secretInformer,
		ConfigMapInformer: configMapInformer,
		ServiceInformer:   serviceInformer,
	})
	if err != nil {
		return nil, err
	}

	go podInformerFactory.Start(context.TODO().Done())
	go scmInformerFactory.Start(context.TODO().Done())

	go nodeRunner.Run(context.TODO())
	// err = nodeRunner.Run(context.TODO())
	// if err != nil {
	// 	panic(err)
	// }
	// log.Println("RUnning...")

	go podRunner.Run(context.TODO(), 1)
	log.Println("Starting...")
	<-nodeRunner.Ready()
	log.Println("Node ready!")

	return &EdgeNode{
		NodeName:   nodeName,
		Kubernetes: kubernetes,
		Podman:     podman,

		NodeRunner: nodeRunner,
		PodRunner:  podRunner,

		PodInformer:       podInformer,
		SecretInformer:    secretInformer,
		ConfigMapInformer: configMapInformer,
		ServiceInformer:   serviceInformer,
	}, nil

	// select {
	// case <-podRunner.Ready():
	// 	log.Println("Ready...")
	// 	<-podRunner.Done()
	// 	log.Println("Done!")
	// case <-podRunner.Done():
	// 	log.Println("Done...")
	// }
	// if podRunner.Err() != nil {
	// 	log.Println(podRunner.Err())
	// 	// handle error
	// }
	// log.Println("exit")
}
