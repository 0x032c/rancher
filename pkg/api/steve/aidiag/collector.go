package aidiag

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ResourceInfo holds the collected diagnostic context for a resource.
type ResourceInfo struct {
	Kind       string
	Name       string
	Namespace  string
	Status     string
	Events     []string
	Logs       []string
	Conditions []string
	Extra      map[string]string
}

type Collector struct {
	client kubernetes.Interface
}

func NewCollector(client kubernetes.Interface) *Collector {
	return &Collector{client: client}
}

func (c *Collector) Collect(ctx context.Context, kind, namespace, name string) (*ResourceInfo, error) {
	switch strings.ToLower(kind) {
	case "pod":
		return c.collectPod(ctx, namespace, name)
	case "deployment":
		return c.collectDeployment(ctx, namespace, name)
	case "statefulset":
		return c.collectStatefulSet(ctx, namespace, name)
	case "daemonset":
		return c.collectDaemonSet(ctx, namespace, name)
	case "node":
		return c.collectNode(ctx, name)
	default:
		return c.collectGeneric(ctx, kind, namespace, name)
	}
}

func (c *Collector) collectPod(ctx context.Context, namespace, name string) (*ResourceInfo, error) {
	pod, err := c.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s/%s: %w", namespace, name, err)
	}

	info := &ResourceInfo{
		Kind:      "Pod",
		Name:      name,
		Namespace: namespace,
		Status:    string(pod.Status.Phase),
		Extra:     make(map[string]string),
	}

	for _, cond := range pod.Status.Conditions {
		info.Conditions = append(info.Conditions,
			fmt.Sprintf("%s=%s (reason=%s, message=%s)", cond.Type, cond.Status, cond.Reason, cond.Message))
	}

	for _, cs := range pod.Status.ContainerStatuses {
		info.Extra[fmt.Sprintf("container/%s/ready", cs.Name)] = fmt.Sprintf("%v", cs.Ready)
		info.Extra[fmt.Sprintf("container/%s/restartCount", cs.Name)] = fmt.Sprintf("%d", cs.RestartCount)
		if cs.State.Waiting != nil {
			info.Extra[fmt.Sprintf("container/%s/waiting", cs.Name)] =
				fmt.Sprintf("reason=%s, message=%s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		}
		if cs.State.Terminated != nil {
			info.Extra[fmt.Sprintf("container/%s/terminated", cs.Name)] =
				fmt.Sprintf("reason=%s, exitCode=%d, message=%s",
					cs.State.Terminated.Reason, cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
		}
		if cs.LastTerminationState.Terminated != nil {
			info.Extra[fmt.Sprintf("container/%s/lastTerminated", cs.Name)] =
				fmt.Sprintf("reason=%s, exitCode=%d", cs.LastTerminationState.Terminated.Reason, cs.LastTerminationState.Terminated.ExitCode)
		}
	}

	events, _ := c.getEvents(ctx, namespace, "Pod", name, pod.UID)
	info.Events = events

	logs := c.getPodLogs(ctx, namespace, name, pod)
	info.Logs = logs

	return info, nil
}

func (c *Collector) collectDeployment(ctx context.Context, namespace, name string) (*ResourceInfo, error) {
	deploy, err := c.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
	}

	info := &ResourceInfo{
		Kind:      "Deployment",
		Name:      name,
		Namespace: namespace,
		Status:    fmt.Sprintf("desired=%d, ready=%d, available=%d, unavailable=%d", *deploy.Spec.Replicas, deploy.Status.ReadyReplicas, deploy.Status.AvailableReplicas, deploy.Status.UnavailableReplicas),
		Extra:     make(map[string]string),
	}

	for _, cond := range deploy.Status.Conditions {
		info.Conditions = append(info.Conditions,
			fmt.Sprintf("%s=%s (reason=%s, message=%s)", cond.Type, cond.Status, cond.Reason, cond.Message))
	}

	events, _ := c.getEvents(ctx, namespace, "Deployment", name, deploy.UID)
	info.Events = events

	return info, nil
}

func (c *Collector) collectStatefulSet(ctx context.Context, namespace, name string) (*ResourceInfo, error) {
	sts, err := c.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get statefulset %s/%s: %w", namespace, name, err)
	}

	info := &ResourceInfo{
		Kind:      "StatefulSet",
		Name:      name,
		Namespace: namespace,
		Status:    fmt.Sprintf("desired=%d, ready=%d, current=%d", *sts.Spec.Replicas, sts.Status.ReadyReplicas, sts.Status.CurrentReplicas),
		Extra:     make(map[string]string),
	}

	for _, cond := range sts.Status.Conditions {
		info.Conditions = append(info.Conditions,
			fmt.Sprintf("%s=%s (reason=%s, message=%s)", cond.Type, cond.Status, cond.Reason, cond.Message))
	}

	events, _ := c.getEvents(ctx, namespace, "StatefulSet", name, sts.UID)
	info.Events = events

	return info, nil
}

func (c *Collector) collectDaemonSet(ctx context.Context, namespace, name string) (*ResourceInfo, error) {
	ds, err := c.client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get daemonset %s/%s: %w", namespace, name, err)
	}

	info := &ResourceInfo{
		Kind:      "DaemonSet",
		Name:      name,
		Namespace: namespace,
		Status:    fmt.Sprintf("desired=%d, ready=%d, available=%d, unavailable=%d", ds.Status.DesiredNumberScheduled, ds.Status.NumberReady, ds.Status.NumberAvailable, ds.Status.NumberUnavailable),
		Extra:     make(map[string]string),
	}

	events, _ := c.getEvents(ctx, namespace, "DaemonSet", name, ds.UID)
	info.Events = events

	return info, nil
}

func (c *Collector) collectNode(ctx context.Context, name string) (*ResourceInfo, error) {
	node, err := c.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", name, err)
	}

	info := &ResourceInfo{
		Kind:  "Node",
		Name:  name,
		Extra: make(map[string]string),
	}

	for _, cond := range node.Status.Conditions {
		info.Conditions = append(info.Conditions,
			fmt.Sprintf("%s=%s (reason=%s, message=%s)", cond.Type, cond.Status, cond.Reason, cond.Message))
	}

	info.Extra["kubeletVersion"] = node.Status.NodeInfo.KubeletVersion
	info.Extra["containerRuntime"] = node.Status.NodeInfo.ContainerRuntimeVersion
	info.Extra["os"] = node.Status.NodeInfo.OSImage
	if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
		info.Extra["allocatable/cpu"] = cpu.String()
	}
	if mem := node.Status.Allocatable.Memory(); mem != nil {
		info.Extra["allocatable/memory"] = mem.String()
	}

	events, _ := c.getEvents(ctx, "", "Node", name, node.UID)
	info.Events = events

	return info, nil
}

func (c *Collector) collectGeneric(ctx context.Context, kind, namespace, name string) (*ResourceInfo, error) {
	info := &ResourceInfo{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		Extra:     make(map[string]string),
	}

	events, _ := c.getEvents(ctx, namespace, kind, name, "")
	info.Events = events

	return info, nil
}

func (c *Collector) getEvents(ctx context.Context, namespace, kind, name string, uid interface{}) ([]string, error) {
	fieldSelector := fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=%s", name, kind)
	events, err := c.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
		Limit:         30,
	})
	if err != nil {
		return nil, err
	}

	var result []string
	for _, e := range events.Items {
		result = append(result,
			fmt.Sprintf("[%s] %s: %s (count=%d)", e.Type, e.Reason, e.Message, e.Count))
	}
	return result, nil
}

func (c *Collector) getPodLogs(ctx context.Context, namespace, name string, pod *corev1.Pod) []string {
	var allLogs []string
	tailLines := int64(100)

	for _, container := range pod.Spec.Containers {
		opts := &corev1.PodLogOptions{
			Container: container.Name,
			TailLines: &tailLines,
		}
		req := c.client.CoreV1().Pods(namespace).GetLogs(name, opts)
		stream, err := req.Stream(ctx)
		if err != nil {
			allLogs = append(allLogs, fmt.Sprintf("--- container/%s: failed to get logs: %v ---", container.Name, err))
			continue
		}
		logBytes, err := io.ReadAll(io.LimitReader(stream, 64*1024))
		stream.Close()
		if err != nil {
			allLogs = append(allLogs, fmt.Sprintf("--- container/%s: error reading logs: %v ---", container.Name, err))
			continue
		}
		allLogs = append(allLogs, fmt.Sprintf("--- container/%s logs (last %d lines) ---\n%s", container.Name, tailLines, string(logBytes)))
	}

	return allLogs
}
