// +build !ignore_autogenerated

// Code generated by operator-sdk. DO NOT EDIT.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CPUAndMem) DeepCopyInto(out *CPUAndMem) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CPUAndMem.
func (in *CPUAndMem) DeepCopy() *CPUAndMem {
	if in == nil {
		return nil
	}
	out := new(CPUAndMem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClickHouseCluster) DeepCopyInto(out *ClickHouseCluster) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClickHouseCluster.
func (in *ClickHouseCluster) DeepCopy() *ClickHouseCluster {
	if in == nil {
		return nil
	}
	out := new(ClickHouseCluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClickHouseCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClickHouseClusterList) DeepCopyInto(out *ClickHouseClusterList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ClickHouseCluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClickHouseClusterList.
func (in *ClickHouseClusterList) DeepCopy() *ClickHouseClusterList {
	if in == nil {
		return nil
	}
	out := new(ClickHouseClusterList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ClickHouseClusterList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClickHouseClusterSpec) DeepCopyInto(out *ClickHouseClusterSpec) {
	*out = *in
	if in.Zookeeper != nil {
		in, out := &in.Zookeeper, &out.Zookeeper
		*out = new(ZookeeperConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Pod != nil {
		in, out := &in.Pod, &out.Pod
		*out = new(PodPolicy)
		(*in).DeepCopyInto(*out)
	}
	out.Resources = in.Resources
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClickHouseClusterSpec.
func (in *ClickHouseClusterSpec) DeepCopy() *ClickHouseClusterSpec {
	if in == nil {
		return nil
	}
	out := new(ClickHouseClusterSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClickHouseClusterStatus) DeepCopyInto(out *ClickHouseClusterStatus) {
	*out = *in
	if in.ShardStatus != nil {
		in, out := &in.ShardStatus, &out.ShardStatus
		*out = make(map[string]*ShardStatus, len(*in))
		for key, val := range *in {
			var outVal *ShardStatus
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(ShardStatus)
				**out = **in
			}
			(*out)[key] = outVal
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClickHouseClusterStatus.
func (in *ClickHouseClusterStatus) DeepCopy() *ClickHouseClusterStatus {
	if in == nil {
		return nil
	}
	out := new(ClickHouseClusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClickHouseResources) DeepCopyInto(out *ClickHouseResources) {
	*out = *in
	out.Requests = in.Requests
	out.Limits = in.Limits
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClickHouseResources.
func (in *ClickHouseResources) DeepCopy() *ClickHouseResources {
	if in == nil {
		return nil
	}
	out := new(ClickHouseResources)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CustomPodSpec) DeepCopyInto(out *CustomPodSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CustomPodSpec.
func (in *CustomPodSpec) DeepCopy() *CustomPodSpec {
	if in == nil {
		return nil
	}
	out := new(CustomPodSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodPolicy) DeepCopyInto(out *PodPolicy) {
	*out = *in
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Affinity != nil {
		in, out := &in.Affinity, &out.Affinity
		*out = new(corev1.Affinity)
		(*in).DeepCopyInto(*out)
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodPolicy.
func (in *PodPolicy) DeepCopy() *PodPolicy {
	if in == nil {
		return nil
	}
	out := new(PodPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ShardStatus) DeepCopyInto(out *ShardStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ShardStatus.
func (in *ShardStatus) DeepCopy() *ShardStatus {
	if in == nil {
		return nil
	}
	out := new(ShardStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ZookeeperConfig) DeepCopyInto(out *ZookeeperConfig) {
	*out = *in
	if in.Nodes != nil {
		in, out := &in.Nodes, &out.Nodes
		*out = make([]ZookeeperNode, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ZookeeperConfig.
func (in *ZookeeperConfig) DeepCopy() *ZookeeperConfig {
	if in == nil {
		return nil
	}
	out := new(ZookeeperConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ZookeeperNode) DeepCopyInto(out *ZookeeperNode) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ZookeeperNode.
func (in *ZookeeperNode) DeepCopy() *ZookeeperNode {
	if in == nil {
		return nil
	}
	out := new(ZookeeperNode)
	in.DeepCopyInto(out)
	return out
}
