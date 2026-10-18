package runner

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const k8sPortForwardReadyTimeout = 30 * time.Second

func k8sForwardKey(executionID, nodeID, containerPort string) string {
	return executionID + "|" + nodeID + "|" + containerPort
}

// HostPort returns the host port forwarded to a service pod's container port,
// or "" when that step has no forward.
//
// Plugin steps run in the suite's APISIX sidecar, a Docker container outside
// the cluster, and dial sibling services by name — consumer-lag's
// "http://broker:8082", llm-judge's "http://ollama:11434". On Docker the
// runtime rewrites those to the container's published port; a pod has none,
// so its service step forwards one.
func (k *Kubernetes) HostPort(executionID, nodeID, containerPort string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.forwards[k8sForwardKey(executionID, nodeID, containerPort)]
}

// forwardServicePorts opens a forward from a free 127.0.0.1 port to each of
// the pod's ports in step.PublishPorts — the ports a plugin references.
// Loopback is enough: Docker Desktop routes host.docker.internal from a
// container to the host's loopback, which is how the sidecar reaches it.
func (k *Kubernetes) forwardServicePorts(client kubernetes.Interface, step StepSpec, podName string) error {
	if len(step.PublishPorts) == 0 {
		return nil
	}
	cfg, err := k.restConfig()
	if err != nil {
		return err
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return fmt.Errorf("port-forward transport: %w", err)
	}

	for _, published := range step.PublishPorts {
		remote := strings.TrimSuffix(strings.TrimSpace(published), "/tcp")
		if _, err := strconv.Atoi(remote); err != nil {
			continue
		}
		req := client.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(k.namespace).
			Name(podName).
			SubResource("portforward")
		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

		stopCh := make(chan struct{})
		readyCh := make(chan struct{})
		pf, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{"0:" + remote}, stopCh, readyCh, io.Discard, io.Discard)
		if err != nil {
			return fmt.Errorf("port-forward %s: %w", remote, err)
		}
		errCh := make(chan error, 1)
		go func() { errCh <- pf.ForwardPorts() }()

		select {
		case <-readyCh:
		case err := <-errCh:
			return fmt.Errorf("port-forward %s: %w", remote, err)
		case <-time.After(k8sPortForwardReadyTimeout):
			close(stopCh)
			return fmt.Errorf("port-forward %s: not ready after %s", remote, k8sPortForwardReadyTimeout)
		}

		ports, err := pf.GetPorts()
		if err != nil || len(ports) == 0 {
			close(stopCh)
			return fmt.Errorf("port-forward %s: no local port", remote)
		}
		k.registerForward(step.ExecutionID, k8sForwardKey(step.ExecutionID, step.Node.ID, remote+"/tcp"), strconv.Itoa(int(ports[0].Local)), stopCh)
	}
	return nil
}

func (k *Kubernetes) registerForward(executionID, key, hostPort string, stop chan struct{}) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.forwards == nil {
		k.forwards = make(map[string]string)
		k.stops = make(map[string][]chan struct{})
	}
	k.forwards[key] = hostPort
	k.stops[executionID] = append(k.stops[executionID], stop)
}

// closeForwards stops every forward the execution opened.
func (k *Kubernetes) closeForwards(executionID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, stop := range k.stops[executionID] {
		close(stop)
	}
	delete(k.stops, executionID)
	prefix := executionID + "|"
	for key := range k.forwards {
		if strings.HasPrefix(key, prefix) {
			delete(k.forwards, key)
		}
	}
}
