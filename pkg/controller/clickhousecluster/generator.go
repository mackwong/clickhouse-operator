package clickhousecluster

import (
	"encoding/json"
	"fmt"
	"regexp"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	clickhousev1 "github.com/mackwong/clickhouse-operator/pkg/apis/clickhouse/v1"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// ClickHouse open ports
	chDefaultExporterPortName      = "exporter"
	chDefaultExporterPortNumber    = 9363
	chDefaultHTTPPortName          = "http"
	chDefaultHTTPPortNumber        = 8123
	chDefaultClientPortName        = "client"
	chDefaultClientPortNumber      = 9000
	chDefaultInterServerPortName   = "interserver"
	chDefaultInterServerPortNumber = 9009

	ClickHouseContainerName = "clickhouse"
	//ClickHouseExporterContainerName = "exporter"
	InitContainerName = "clickhouse-init"

	filenameRemoteServersXML = "remote_servers.xml"
	filenameAllMacrosJSON    = "all-macros.json"
	filenameUsersXML         = "users.xml"
	filenameZookeeperXML     = "zookeeper.xml"
	filenameSettingsXML      = "settings.xml"

	dirPathConfigd = "/etc/clickhouse-server/config.d/"
	dirPathUsersd  = "/etc/clickhouse-server/users.d/"
	dirPathConfd   = "/etc/clickhouse-server/conf.d/"
	dirPathData    = "/var/lib/clickhouse/"

	pspName = "clickhouse-operator"

	macrosTemplate = `
<yandex>
        <macros>
            <cluster>%s</cluster>
            <shard>%d</shard>
            <replica>%s</replica>
        </macros>
</yandex>`
)

type Generator struct {
	rcc *ReconcileClickHouseCluster
	cc  *clickhousev1.ClickHouseCluster
}

func NewGenerator(rcc *ReconcileClickHouseCluster, cc *clickhousev1.ClickHouseCluster) *Generator {
	return &Generator{rcc: rcc, cc: cc}
}

func (g *Generator) labelsForStatefulSet(shardID int, chcLabels map[string]string) map[string]string {
	labels := map[string]string{
		CreateByLabelKey: OperatorLabelKey,
		ClusterLabelKey:  g.cc.Name,
		ShardIDLabelKey:  fmt.Sprintf("%d", shardID),
	}
	for k, v := range chcLabels {
		labels[k] = v
	}
	return labels
}

func (g *Generator) labelsForCluster() map[string]string {
	return map[string]string{
		ClusterLabelKey: g.cc.Name,
	}
}

func (g *Generator) ownerReference() []metav1.OwnerReference {
	ref := metav1.OwnerReference{
		APIVersion: g.cc.APIVersion,
		Kind:       g.cc.Kind,
		Name:       g.cc.Name,
		UID:        g.cc.UID,
	}
	return []metav1.OwnerReference{ref}
}

func (g *Generator) marosEmptyDirName() string {
	return fmt.Sprintf("%s-maros", g.cc.Name)
}

func (g *Generator) commonConfigMapName() string {
	return fmt.Sprintf("%s-common-config", g.cc.Name)
}

func (g *Generator) commonServiceName() string {
	return fmt.Sprintf("%s", g.cc.Name)
}

func (g *Generator) PspRoleBindingName() string {
	return fmt.Sprintf("psp-%s", g.cc.Name)
}

func (g *Generator) userConfigMapName() string {
	return fmt.Sprintf("%s-user-config", g.cc.Name)
}

func (g *Generator) volumeClaimName() string {
	return fmt.Sprintf("%s-volume-claim", g.cc.Name)
}

func (g *Generator) statefulSetName(shardID int) string {
	return fmt.Sprintf("%s-%d", g.cc.Name, shardID)
}

func (g *Generator) serviceName(shardID int) string {
	return g.statefulSetName(shardID)
}

func (g *Generator) FQDN(shardID, replicasID int, namespace string) string {
	statefulset := g.statefulSetName(shardID)
	serviceName := g.serviceName(shardID)
	return fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local", statefulset, replicasID, serviceName, namespace)
}

func (g *Generator) FQDNs() []string {
	hosts := make([]string, 0)
	shards := make([]Shard, g.cc.Spec.ShardsCount)
	for i := range shards {
		for j := 0; j < int(g.cc.Spec.ReplicasCount); j++ {
			host := g.FQDN(i, j, g.cc.Namespace)
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func (g *Generator) generateRemoteServersXML() string {
	shards := make([]Shard, g.cc.Spec.ShardsCount)
	for i := range shards {
		replicas := make([]Replica, g.cc.Spec.ReplicasCount)
		for j := range replicas {
			replicas[j].Host = g.FQDN(i, j, g.cc.Namespace)
			replicas[j].Port = chDefaultClientPortNumber
			for user, password := range g.getUserAndPassword() {
				replicas[j].User = user
				replicas[j].Password = password
			}
		}
		shards[i].InternalReplication = false
		shards[i].Replica = replicas
	}

	servers := RemoteServers{RemoteServer: map[string]Cluster{
		g.cc.Name: {shards},
	}}
	return ParseXML(servers)
}

func (g *Generator) getUserAndPassword() map[string]string {
	return decodeUsersXML(g.cc.Spec.Users)
}

func (g *Generator) generateZookeeperXML() string {
	// no zookeeper specified
	if g.cc.Spec.Zookeeper == nil {
		return "<yandex></yandex>"
	}
	for _, node := range g.cc.Spec.Zookeeper.Nodes {
		if "" == node.Host {
			logrus.Debug("node in zookeeper is null, skip to create zookeeper.xml")
			return "<yandex></yandex>"
		}
	}
	zk := Zookeeper{Zookeeper: g.cc.Spec.Zookeeper}
	return ParseXML(zk)
}

func (g *Generator) generateSettingsXML() string {
	//settings := g.cc.Spec.CustomSettings
	//settings = strings.TrimSpace(settings)
	//if strings.Contains(settings, "<disable_internal_dns_cache>1</disable_internal_dns_cache>") != true {
	//	settings = strings.Trim(settings, "</yandex>")
	//	settings = strings.Trim(settings, "<yandex>")
	//	settings = "<yandex>" + "\n\t<disable_internal_dns_cache>1</disable_internal_dns_cache> " + settings + "</yandex>"
	//}
	//return fmt.Sprint(settings)
	return g.cc.Spec.CustomSettings
}

func (g *Generator) generateAllMacrosJson() string {
	macros := make(map[string]string)
	var shardsCount = int(g.cc.Spec.ShardsCount)
	var replicasCount = int(g.cc.Spec.ReplicasCount)
	for i := 0; i < shardsCount; i++ {
		for j := 0; j < replicasCount; j++ {
			replica := fmt.Sprintf("%s-%d", g.statefulSetName(i), j)
			macros[replica] = fmt.Sprintf(macrosTemplate, g.cc.Name, i, replica)
		}
	}
	out, err := json.MarshalIndent(macros, " ", "")
	if err != nil {
		logrus.WithFields(logrus.Fields{"err": err}).Error("Marshal error")
		return ""
	}
	return string(out)
}

func (g *Generator) generateUsersXMl() string {
	return g.cc.Spec.Users
}

func (g *Generator) getPassword() string {
	pwd, _ := regexp.Compile(`<password>(.*)</password>`)
	out := pwd.FindAllStringSubmatch(g.cc.Spec.Users, -1)
	if len(out) > 0 {
		return out[0][1]
	}
	return ""
}

func (g *Generator) GenerateRoleBinding() *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            g.PspRoleBindingName(),
			Namespace:       g.cc.Namespace,
			Labels:          g.labelsForCluster(),
			OwnerReferences: g.ownerReference(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Namespace: g.cc.Namespace,
				Name:      "default",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     pspName,
		},
	}
}

func (g *Generator) GenerateCommonConfigMap() *corev1.ConfigMap {

	data := map[string]string{
		filenameRemoteServersXML: g.generateRemoteServersXML(),
		filenameAllMacrosJSON:    g.generateAllMacrosJson(),
		filenameSettingsXML:      g.generateSettingsXML(),
		filenameZookeeperXML:     g.generateZookeeperXML(),
	}
	for filename, content := range g.rcc.defaultConfig.GetDefaultXMLConfig() {
		data[filename] = content
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            g.commonConfigMapName(),
			Namespace:       g.cc.Namespace,
			Labels:          g.labelsForCluster(),
			OwnerReferences: g.ownerReference(),
		},
		// Data contains several sections which are to be several xml chopConfig files
		Data: data,
	}
}

func (g *Generator) generateUserConfigMap() *corev1.ConfigMap {
	data := map[string]string{
		filenameUsersXML: g.generateUsersXMl(),
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            g.userConfigMapName(),
			Namespace:       g.cc.Namespace,
			Labels:          g.labelsForCluster(),
			OwnerReferences: g.ownerReference(),
			Annotations: map[string]string{
				"password": g.getPassword(),
			},
		},
		// Data contains several sections which are to be several xml chopConfig files
		Data: data,
	}
}

func (g *Generator) generateCommonService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            g.commonServiceName(),
			Namespace:       g.cc.Namespace,
			Labels:          g.labelsForCluster(),
			OwnerReferences: g.ownerReference(),
		},
		Spec: corev1.ServiceSpec{
			// ClusterIP: templateDefaultsServiceClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     chDefaultHTTPPortName,
					Port:     chDefaultHTTPPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultHTTPPortNumber,
					},
				},
				{
					Name:     chDefaultClientPortName,
					Port:     chDefaultClientPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultClientPortNumber,
					},
				},
				{
					Name:     chDefaultExporterPortName,
					Port:     chDefaultExporterPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultExporterPortNumber,
					},
				},
			},
			Selector: g.labelsForCluster(),
			Type:     "ClusterIP",
		},
	}
}

func (g *Generator) generateShardService(shardID int, statefulset *appsv1.StatefulSet) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      g.serviceName(shardID),
			Namespace: g.cc.Namespace,
			Labels:    g.labelsForStatefulSet(shardID, g.cc.Labels),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: statefulset.APIVersion,
				Kind:       statefulset.Kind,
				Name:       statefulset.Name,
				UID:        statefulset.UID,
			}},
		},
		Spec: corev1.ServiceSpec{
			// ClusterIP: templateDefaultsServiceClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:     chDefaultHTTPPortName,
					Port:     chDefaultHTTPPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultHTTPPortNumber,
					},
				},
				{
					Name:     chDefaultClientPortName,
					Port:     chDefaultClientPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultClientPortNumber,
					},
				},
				{
					Name:     chDefaultExporterPortName,
					Port:     chDefaultExporterPortNumber,
					Protocol: "TCP",
					TargetPort: intstr.IntOrString{
						IntVal: chDefaultExporterPortNumber,
					},
				},
			},
			Selector:        g.labelsForStatefulSet(shardID, g.cc.Labels),
			ClusterIP:       "None",
			SessionAffinity: "None",
			Type:            "ClusterIP",
		},
	}
}

func (g *Generator) setupStatefulSetPodTemplate(statefulset *appsv1.StatefulSet, shardID int) {
	statefulset.Spec.Template = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:   statefulset.Name,
			Labels: g.labelsForStatefulSet(shardID, g.cc.Labels),
		},
	}
	statefulset.Spec.Template.Spec = corev1.PodSpec{
		Volumes:       []corev1.Volume{},
		RestartPolicy: "Always",
	}
	if g.cc.Spec.Pod != nil {
		statefulset.Spec.Template.Annotations = g.cc.Spec.Pod.Annotations
		statefulset.Spec.Template.Spec.Tolerations = g.cc.Spec.Pod.Tolerations
		statefulset.Spec.Template.Spec.Affinity = g.cc.Spec.Pod.Affinity
		statefulset.Spec.Template.Spec.NodeSelector = g.cc.Spec.Pod.NodeSelector
	}
	statefulset.Spec.Template.Spec.InitContainers = []corev1.Container{
		{
			Name:  InitContainerName,
			Image: g.cc.Spec.InitImage,
			Env: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath:  "metadata.name",
							APIVersion: "v1",
						},
					},
				},
			},
			Args: []string{"init"},
		},
	}
	statefulset.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Name:  ClickHouseContainerName,
			Image: g.cc.Spec.Image,
			Env: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath:  "metadata.name",
							APIVersion: "v1",
						},
					},
				},
			},
			Ports: []corev1.ContainerPort{
				{
					Name:          chDefaultHTTPPortName,
					ContainerPort: chDefaultHTTPPortNumber,
					Protocol:      "TCP",
				},
				{
					Name:          chDefaultClientPortName,
					ContainerPort: chDefaultClientPortNumber,
					Protocol:      "TCP",
				},
				{
					Name:          chDefaultInterServerPortName,
					ContainerPort: chDefaultInterServerPortNumber,
					Protocol:      "TCP",
				},
			},
			ReadinessProbe: &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/ping",
						Port: intstr.Parse(chDefaultHTTPPortName),
					},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       10,
			},
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{
						"NET_ADMIN",
						"SYS_NICE",
					},
				},
			},
			Resources: corev1.ResourceRequirements{
				Requests: generateResourceList(g.cc.Spec.Resources.Requests),
				Limits:   generateResourceList(g.cc.Spec.Resources.Limits),
			},
		},
	}

	// Add all ConfigMap objects as Volume objects of type ConfigMap
	statefulset.Spec.Template.Spec.Volumes = append(
		statefulset.Spec.Template.Spec.Volumes,
		newVolumeForConfigMap(g.commonConfigMapName()),
		newVolumeForEmptyDir(g.marosEmptyDirName()),
		newVolumeForConfigMap(g.userConfigMapName()),
	)

	// And reference these Volumes in each Container via VolumeMount
	// So Pod will have ConfigMaps mounted as Volumes
	for i := range statefulset.Spec.Template.Spec.Containers {
		// Convenience wrapper
		container := &statefulset.Spec.Template.Spec.Containers[i]
		if container.Name != ClickHouseContainerName {
			continue
		}
		// Append to each Container current VolumeMount's to VolumeMount's declared in template
		container.VolumeMounts = append(
			container.VolumeMounts,
			newVolumeMount(g.commonConfigMapName(), dirPathConfigd),
			newVolumeMount(g.marosEmptyDirName(), dirPathConfd),
			newVolumeMount(g.userConfigMapName(), dirPathUsersd),
		)
	}

	for i := range statefulset.Spec.Template.Spec.InitContainers {
		// Convenience wrapper
		container := &statefulset.Spec.Template.Spec.InitContainers[i]
		// Append to each Container current VolumeMount's to VolumeMount's declared in template
		container.VolumeMounts = append(
			container.VolumeMounts,
			newVolumeMount(g.commonConfigMapName(), dirPathConfigd),
			newVolumeMount(g.marosEmptyDirName(), dirPathConfd),
			//newVolumeMount(g.userConfigMapName(), dirPathUsersd),
		)
	}
}

func (g *Generator) setupStatefulSetVolumeClaimTemplates(statefulSet *appsv1.StatefulSet, dataStorageClass, dataCapacity string) {
	quantity, _ := resource.ParseQuantity(dataCapacity)
	statefulSet.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: g.volumeClaimName(),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				StorageClassName: &dataStorageClass,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: quantity,
					},
				},
			},
		},
	}

	for i := range statefulSet.Spec.Template.Spec.Containers {
		// Convenience wrapper
		container := &statefulSet.Spec.Template.Spec.Containers[i]
		container.VolumeMounts = append(container.VolumeMounts, newVolumeMount(g.volumeClaimName(), dirPathData))
	}

}

func (g *Generator) generateStatefulSet(shardID int) *appsv1.StatefulSet {
	// Create apps.StatefulSet object
	replicasNum := g.cc.Spec.ReplicasCount
	// StatefulSet has additional label - ZK config fingerprint
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            g.statefulSetName(shardID),
			Namespace:       g.cc.Namespace,
			Annotations:     make(map[string]string),
			Labels:          g.labelsForStatefulSet(shardID, g.cc.Labels),
			OwnerReferences: g.ownerReference(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicasNum,
			//ServiceName: g.serviceName(shardID),
			Selector: &metav1.LabelSelector{
				MatchLabels: g.labelsForStatefulSet(shardID, g.cc.Labels),
			},
			ServiceName: g.serviceName(shardID),
			// IMPORTANT
			// VolumeClaimTemplates are to be setup later
			VolumeClaimTemplates: nil,

			// IMPORTANT
			// Template is to be setup later
			Template: corev1.PodTemplateSpec{},
		},
	}

	g.setupStatefulSetPodTemplate(statefulSet, shardID)
	if g.cc.Spec.DataStorageClass != "" {
		g.setupStatefulSetVolumeClaimTemplates(statefulSet, g.cc.Spec.DataStorageClass, g.cc.Spec.DataCapacity)
	}

	return statefulSet
}

func (g *Generator) generateServiceMonitor() *monitoringv1.ServiceMonitor {
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clickhouse-" + g.cc.Name,
			Namespace: g.cc.Namespace,
			Labels: map[string]string{
				"paas-component": "clickhouse",
				"source":         "paas-monitoring",
				"prometheus":     "kube-prometheus",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "clickhouse.service.diamond.sensetime.com/v1",
					Kind:       "ClickHouseCluster",
					Name:       g.cc.Name,
					UID:        g.cc.UID,
				},
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					ClusterLabelKey:  g.cc.Name,
					CreateByLabelKey: OperatorLabelKey,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "exporter",
					Path:     "/metrics",
					Interval: "15s",
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{MatchNames: []string{g.cc.Namespace}},
			PodTargetLabels:   []string{"instance_name"},
		},
	}
	return sm
}

// newVolumeForConfigMap returns corev1.Volume object with defined name
func newVolumeForConfigMap(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
				DefaultMode: &[]int32{420}[0],
			},
		},
	}
}

func newVolumeForEmptyDir(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// newVolumeMount returns corev1.VolumeMount object with name and mount path
func newVolumeMount(name, mountPath string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      name,
		MountPath: mountPath,
	}
}
