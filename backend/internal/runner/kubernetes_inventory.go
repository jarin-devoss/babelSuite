package runner

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Inventory lists the executions the backend's namespace holds: their pods,
// the headless Service that gives them DNS, and the workspace claim — every
// object SetupExecution and the steps create carries the execution label.
func (k *Kubernetes) Inventory(ctx context.Context) ([]ExecutionInventory, error) {
	client, err := k.newClient()
	if err != nil {
		return nil, fmt.Errorf("kubernetes client unavailable: %w", err)
	}
	selector := metav1.ListOptions{LabelSelector: k8sExecutionLabel}

	pods, err := client.CoreV1().Pods(k.namespace).List(ctx, selector)
	if err != nil {
		return nil, err
	}
	services, err := client.CoreV1().Services(k.namespace).List(ctx, selector)
	if err != nil {
		return nil, err
	}
	claims, err := client.CoreV1().PersistentVolumeClaims(k.namespace).List(ctx, selector)
	if err != nil {
		return nil, err
	}

	groups := inventoryGroups{}
	for _, pod := range pods.Items {
		run := groups.run(pod.Labels[k8sExecutionLabel], pod.Labels[labelSuite], pod.Labels[labelProfile])
		image := ""
		if len(pod.Spec.Containers) > 0 {
			image = pod.Spec.Containers[0].Image
		}
		item := InventoryItem{
			ID: string(pod.UID), Name: pod.Name, Kind: "pod", Image: image,
			State: strings.ToLower(string(pod.Status.Phase)),
		}
		if pod.Status.StartTime != nil {
			started := pod.Status.StartTime.UTC()
			item.StartedAt = &started
		}
		run.Containers = append(run.Containers, item)
	}
	for _, svc := range services.Items {
		run := groups.run(svc.Labels[k8sExecutionLabel], "", "")
		run.Networks = append(run.Networks, InventoryItem{ID: string(svc.UID), Name: svc.Name, Kind: "headless-service"})
	}
	for _, pvc := range claims.Items {
		run := groups.run(pvc.Labels[k8sExecutionLabel], "", "")
		run.Volumes = append(run.Volumes, InventoryItem{ID: string(pvc.UID), Name: pvc.Name, Kind: "persistentvolumeclaim"})
	}
	return groups.list(), nil
}

// Reap removes an execution's pods, Service and claim — the cleanup that runs
// when an execution ends, for one whose control plane never got to run it.
func (k *Kubernetes) Reap(ctx context.Context, runID string) error {
	return k.DestroyExecution(ctx, runID)
}

// k8sLabelValue fits s to a label value: at most 63 characters of
// alphanumerics, '-', '_' and '.', starting and ending with an alphanumeric.
func k8sLabelValue(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_', ch == '.':
			b.WriteRune(ch)
		default:
			b.WriteRune('-')
		}
	}
	s = b.String()
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.Trim(s, "-_.")
}
