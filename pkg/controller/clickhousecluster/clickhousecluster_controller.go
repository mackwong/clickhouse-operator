package clickhousecluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/samuel/go-zookeeper/zk"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/mackwong/clickhouse-operator/pkg/config"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	clickhousev1 "github.com/mackwong/clickhouse-operator/pkg/apis/clickhouse/v1"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var needUpdate bool

const defaultUserXML = `
<yandex>
  <users>
     <default>
        <password>%s</password>
        <access_management>1</access_management>
     </default>
  </users>
</yandex>
`

// Add creates a new ClickHouseCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcileShard.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	defaultConfig, err := config.LoadDefaultConfig()
	if err != nil {
		logrus.WithFields(logrus.Fields{"error": err}).Fatal("Load default config error")
	}
	return &ReconcileClickHouseCluster{client: mgr.GetClient(), scheme: mgr.GetScheme(), defaultConfig: defaultConfig}
}

// add adds a new Controller to mgr with r as the reconcileShard.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("clickhousecluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ClickHouseCluster
	err = c.Watch(&source.Kind{Type: &clickhousev1.ClickHouseCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner ClickHouseCluster
	//err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
	//	IsController: true,
	//	OwnerType:    &clickhousev1.ClickHouseCluster{},
	//})
	//if err != nil {
	//	return err
	//}

	return nil
}

// blank assignment to verify that ReconcileClickHouseCluster implements reconcileShard.Reconciler
var _ reconcile.Reconciler = &ReconcileClickHouseCluster{}

// ReconcileClickHouseCluster reconciles a ClickHouseCluster object
type ReconcileClickHouseCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	defaultConfig *config.DefaultConfig
}

func (r *ReconcileClickHouseCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	log := logrus.WithFields(logrus.Fields{"namespace": request.Namespace, "name": request.Name})

	requeue5 := reconcile.Result{RequeueAfter: 5 * time.Second}
	requeue30 := reconcile.Result{RequeueAfter: 30 * time.Second}
	forget := reconcile.Result{}

	// Fetch the ClickHouseCluster
	cc := &clickhousev1.ClickHouseCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, cc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Delete ClickHouseCluster")
			return forget, nil
		}
		log.WithField("error", err).Error("get clickhouse cluster error")
		return forget, err
	}

	// https://github.com/ClickHouse/ClickHouse/issues/6402
	if cc.DeletionTimestamp != nil {
		wholePath := fmt.Sprintf("default@%s-0-0.%s-0.%s.svc.cluster.local:9000", cc.Name, cc.Name, cc.Namespace)
		if int(cc.Spec.ReplicasCount)*len(wholePath) > 255 {
			log.Warning("the name is too long, may cause failure")
		}
	}

	if cc.Annotations == nil {
		cc.Annotations = map[string]string{ClusterNewCreate: "true"}
	}
	if cc.Status.ShardStatus == nil {
		cc.Status.ShardStatus = make(map[string]*clickhousev1.ShardStatus)
	}

	//Set Default Values
	if cc.Status.Phase == "" {
		needUpdate = r.setDefaults(cc, r.defaultConfig)

		err = r.checkInitZookeeperCfg(cc)
		if err != nil {
			log.WithField("error", err).Error("check zookeeper configuration error")
			// cannot continue until zookeeper is specified
			return forget, err
		}
	}

	r.formatZookeeper(cc)

	err = r.CheckDeletePVC(cc)
	if err != nil {
		log.WithField("error", err).Error("check ClickHouseCluster delete pvc error")
		return forget, err
	}

	err = validateResource(cc)
	if err != nil {
		log.WithField("error", err).Error("validate Resource field error")
		return forget, err
	}

	status := cc.Status.DeepCopy()
	defer r.updateClickHouseStatus(cc, status)

	//Some changes are not allowed, so we need to recovery to old one
	if r.CheckNonAllowedChanges(cc) {
		if err = r.recoveryCRD(cc); err != nil {
			log.WithField("error", err).Error("recovery ClickHouseCluster error")
			return requeue30, err
		}
	}

	var generator = NewGenerator(r, cc)

	serviceMonitor := generator.generateServiceMonitor()
	if err := r.checkServiceMonitor(serviceMonitor); err != nil {
		log.WithField("error", err).Error("check servicemonitor error")
		return forget, err
	}

	roleBinding := generator.GenerateRoleBinding()
	if err := r.reconcileRoleBinding(roleBinding); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": roleBinding.Namespace, "name": roleBinding.Name, "error": err}).Error("create clusterRoleBinding error")
		return requeue5, err
	}

	commonConfigMap := generator.GenerateCommonConfigMap()
	if err := r.reconcileConfigMap(commonConfigMap); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": commonConfigMap.Namespace, "name": commonConfigMap.Name, "error": err}).Error("create common configmap error")
		return requeue5, err
	}

	commonService := generator.generateCommonService()
	if err := r.reconcileService(commonService); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": commonService.Namespace, "name": commonService.Name, "error": err}).Error("create common service error")
		return requeue5, err
	}

	userConfigMap := generator.generateUserConfigMap()
	if err := r.reconcileConfigMap(userConfigMap); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": userConfigMap.Namespace, "name": userConfigMap.Name, "error": err}).Error("create users configmap error")
		return requeue5, err
	}

	for shardID := 0; shardID < int(cc.Spec.ShardsCount); shardID++ {
		var ready bool
		if ready, err = r.reconcileShard(isClusterNewCreate(cc), generator, shardID, status); err != nil {
			log.WithField("error", err).Error("reconcileShard error")
			return requeue5, err
		}
		if !ready {
			return requeue30, nil
		}
	}

	if err = r.ScaleDownCluster(cc); err != nil {
		return requeue30, err
	}

	//log.Info(cc.DeletionTimestamp, cc.Spec.DeletePVC)
	if cc.DeletionTimestamp != nil && cc.Spec.DeletePVC {
		log.Info("deleting pvc")
		if err = r.DeletePVCs(cc); err != nil {
			log.WithField("error", err).Error("delete pvc error")
			return requeue5, err
		}
		// Indicate delete pvc means no longer need the data, delete zookeeper path as well
		log.Info("deleting zookeeper path")
		if err = r.deleteZookeeperPath(cc); err != nil {
			log.WithField("error", err).Error("delete zookeeper path error")
			return requeue5, err
		}
		preventClusterDeletion(cc, false)
		needUpdate = true
	}

	cc.Annotations[ClusterNewCreate] = "false"
	if cc.DeletionTimestamp == nil {
		err := r.createTablesInNewStatefulSet(cc, generator)
		if err != nil {
			return requeue5, err
		}
	}
	//if err := r.createTablesInNewStatefulSet(cc, generator); err != nil {
	//	return requeue5, err
	//}
	return requeue5, nil
}

func (r *ReconcileClickHouseCluster) createTablesInNewStatefulSet(cc *clickhousev1.ClickHouseCluster,
	generator *Generator) error {

	var statefulSets = appsv1.StatefulSetList{}
	err := r.client.List(context.TODO(), &statefulSets, &client.ListOptions{
		Namespace: cc.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			ClusterLabelKey: cc.Name,
		}),
	})
	if err != nil {
		logrus.WithFields(
			logrus.Fields{"namespace": cc.Namespace, "error": err}).
			Error("list statefulset error")
		return err
	}

	var needCreateTable bool

	for _, sts := range statefulSets.Items {
		if sts.Annotations[ClusterHostsChange] == "true" {
			needCreateTable = true
			break
		}
	}
	if !needCreateTable {
		logrus.Debugf("no need to create table for cluster: %s/%s", cc.Namespace, cc.Name)
		return nil
	}

	hosts := generator.FQDNs()
	scr := NewSchemer(cc)
	if err = scr.StatefulSetCreateTables(cc.Name, hosts); err != nil {
		logrus.WithFields(
			logrus.Fields{"namespace": cc.Namespace, "error": err}).
			Error("create table error")
		return err
	}

	for _, sts := range statefulSets.Items {
		sts := sts.DeepCopy()
		if sts.Annotations == nil {
			sts.Annotations = make(map[string]string)
		}
		sts.Annotations[ClusterHostsChange] = "false"
		if err = r.client.Update(context.Background(), sts); err != nil {
			logrus.WithFields(
				logrus.Fields{"namespace": cc.Namespace, "error": err}).
				Error("update statefulset error")
			return err
		}
	}

	return nil
}

func isClusterNewCreate(cc *clickhousev1.ClickHouseCluster) bool {
	return "true" == cc.Annotations[ClusterNewCreate]
}

func preventClusterDeletion(cc *clickhousev1.ClickHouseCluster, value bool) {
	if value {
		cc.SetFinalizers([]string{"kubernetes.io/pvc-to-delete"})
		return
	}
	cc.SetFinalizers([]string{})
}

func updateDeletePvcStrategy(cc *clickhousev1.ClickHouseCluster) {
	logrus.WithFields(logrus.Fields{"cluster": cc.Name, "deletePVC": cc.Spec.DeletePVC,
		"finalizers": cc.Finalizers}).Debug("updateDeletePvcStrategy called")
	// Remove Finalizers if DeletePVC is not enabled
	if !cc.Spec.DeletePVC && len(cc.Finalizers) > 0 {
		logrus.WithFields(logrus.Fields{"cluster": cc.Name}).Info("Won't delete PVCs when nodes are removed")
		preventClusterDeletion(cc, false)
	}
	// Add Finalizer if DeletePVC is enabled
	if cc.Spec.DeletePVC && len(cc.Finalizers) == 0 {
		logrus.WithFields(logrus.Fields{"cluster": cc.Name}).Info("Will delete PVCs when nodes are removed")
		preventClusterDeletion(cc, true)
	}
}

func (r *ReconcileClickHouseCluster) CheckDeletePVC(cc *clickhousev1.ClickHouseCluster) error {
	var oldCRD clickhousev1.ClickHouseCluster
	if cc.Annotations[clickhousev1.AnnotationLastApplied] == "" {
		if cc.DeletionTimestamp == nil && cc.Spec.DeletePVC == true {
			logrus.Info("add finalizer when initial deletePVC is set to true")
			preventClusterDeletion(cc, true)
		}
		return nil
	}

	//We retrieved our last-applied-configuration stored in the CRD
	err := json.Unmarshal([]byte(cc.Annotations[clickhousev1.AnnotationLastApplied]), &oldCRD)
	if err != nil {
		logrus.Errorf("[%s]: Can't get Old version of CRD", cc.Name)
		return nil
	}

	if cc.Spec.DeletePVC != oldCRD.Spec.DeletePVC {
		logrus.WithFields(logrus.Fields{"cluster": cc.Name}).Debug("DeletePVC has been updated")
		updateDeletePvcStrategy(cc)
		return r.client.Update(context.TODO(), cc)
	}
	return nil
}

func (r *ReconcileClickHouseCluster) ScaleDownCluster(cc *clickhousev1.ClickHouseCluster) error {
	var statefulSets = appsv1.StatefulSetList{}
	log := logrus.WithFields(logrus.Fields{"namespace": cc.Namespace, "name": cc.Name})
	err := r.client.List(context.TODO(), &statefulSets, &client.ListOptions{
		Namespace: cc.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			ClusterLabelKey: cc.Name,
		}),
	})
	if err != nil {
		log.WithField("error", err).Error("List statefulset error")
		return err
	}

	for _, statefulSet := range statefulSets.Items {
		shardID, err := strconv.Atoi(statefulSet.Labels["shard-id"])
		if err != nil {
			log.WithField("error", err).Error("Get shard-id error")
			return err
		}
		if shardID >= int(cc.Spec.ShardsCount) {
			err = r.client.Delete(context.TODO(), &statefulSet)
			if err != nil {
				log.WithField("error", err).Error("Delete statefulSet error")
				return err
			}
		}
	}
	return nil
}

func (r *ReconcileClickHouseCluster) updateClickHouseStatus(cc *clickhousev1.ClickHouseCluster, status *clickhousev1.ClickHouseClusterStatus) {
	defer func() {
		needUpdate = false
	}()
	lastApplied, _ := cc.ComputeLastAppliedConfiguration()
	if !needUpdate && reflect.DeepEqual(cc.Status, *status) &&
		reflect.DeepEqual(cc.Annotations[clickhousev1.AnnotationLastApplied], lastApplied) &&
		cc.Annotations[clickhousev1.AnnotationLastApplied] != "" {
		return
	}

	cc.Annotations[clickhousev1.AnnotationLastApplied] = lastApplied
	cc.Status = *status.DeepCopy()

	var readyCount int32
	for _, s := range cc.Status.ShardStatus {
		if s.Phase == ShardPhaseRunning {
			readyCount++
		}
	}
	if readyCount == cc.Spec.ShardsCount {
		cc.Status.Phase = ClusterPhaseRunning
	} else {
		cc.Status.Phase = ClusterPhaseInitial
	}
	err := r.client.Update(context.TODO(), cc)
	if err != nil {
		logrus.WithFields(logrus.Fields{"cluster": cc.Name, "err": err}).Errorf("Issue when updating ClickHouseCluster")
	} else {
		logrus.WithFields(logrus.Fields{"cluster": cc.Name}).Info("Updating ClickHouseCluster")
	}
}

func (r *ReconcileClickHouseCluster) recoveryCRD(cc *clickhousev1.ClickHouseCluster) error {
	var oldCRD clickhousev1.ClickHouseCluster
	if cc.Annotations[clickhousev1.AnnotationLastApplied] == "" {
		return fmt.Errorf("can not find last-applied-configuration")
	}
	err := json.Unmarshal([]byte(cc.Annotations[clickhousev1.AnnotationLastApplied]), &oldCRD)
	if err != nil {
		return err
	}
	cc.Spec = oldCRD.Spec
	return nil
}

func (r *ReconcileClickHouseCluster) reconcileShard(clusterNew bool, generator *Generator, shardID int, status *clickhousev1.ClickHouseClusterStatus) (bool, error) {
	statefulSet := generator.generateStatefulSet(shardID)
	if err := r.reconcileStatefulSet(clusterNew, statefulSet); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": statefulSet.Namespace, "name": statefulSet.Name, "error": err}).Error("create statefulSets error")
		return false, err
	}

	if err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: statefulSet.Namespace, Name: statefulSet.Name}, statefulSet); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": statefulSet.Namespace, "name": statefulSet.Name, "error": err}).Error("get statefulSets error")
		return false, err
	}

	service := generator.generateShardService(shardID, statefulSet)
	if err := r.reconcileService(service); err != nil {
		logrus.WithFields(logrus.Fields{"namespace": service.Namespace, "name": service.Name, "error": err}).Error("create service error")
		return false, err
	}

	if isStatefulSetReady(statefulSet) {
		status.ShardStatus[statefulSet.Name] = &clickhousev1.ShardStatus{Phase: ShardPhaseRunning}
		return true, nil
	} else {
		status.ShardStatus[statefulSet.Name] = &clickhousev1.ShardStatus{Phase: ShardPhaseInitial}
		return false, nil
	}
}

// CheckNonAllowedChanges - checks if there are some changes on CRD that are not allowed on statefulset
// If a non Allowed Changed is Find we won't Update associated kubernetes objects, but we will put back the old value
// and Patch the CRD with correct values
func (r *ReconcileClickHouseCluster) CheckNonAllowedChanges(instance *clickhousev1.ClickHouseCluster) bool {
	var oldCRD clickhousev1.ClickHouseCluster
	if instance.Annotations[clickhousev1.AnnotationLastApplied] == "" {
		return false
	}

	//We retrieved our last-applied-configuration stored in the CRD
	err := json.Unmarshal([]byte(instance.Annotations[clickhousev1.AnnotationLastApplied]), &oldCRD)
	if err != nil {
		logrus.WithFields(logrus.Fields{"cluster": instance.Name}).Error("Can't get Old version of CRD")
		return false
	}
	//DataCapacity change is forbidden
	if instance.Spec.DataCapacity != oldCRD.Spec.DataCapacity {
		logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
			Warningf("The Operator has refused the change on DataCapacity from [%s] to NewValue[%s]",
				oldCRD.Spec.DataCapacity, instance.Spec.DataCapacity)
		instance.Spec.DataCapacity = oldCRD.Spec.DataCapacity
		return true
	}
	//ShardsCount change is forbidden
	if instance.Spec.ShardsCount < oldCRD.Spec.ShardsCount {
		logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
			Warningf("The Operator has refused the reduce ShardsCount from [%d] to NewValue[%d]",
				oldCRD.Spec.ShardsCount, instance.Spec.ShardsCount)
		instance.Spec.ShardsCount = oldCRD.Spec.ShardsCount
		return true
	}
	//Add replicas without zookeeper is forbidden
	if instance.Spec.ReplicasCount > oldCRD.Spec.ReplicasCount && instance.Spec.Zookeeper == nil {
		logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
			Warningf("The Operator has refused the add ReplicasCount from [%d] to NewValue[%d} without zookeeper"+
				"configuration", oldCRD.Spec.ReplicasCount, instance.Spec.ReplicasCount)
		instance.Spec.ReplicasCount = oldCRD.Spec.ReplicasCount
		return true
	}
	//DataStorage
	if instance.Spec.DataStorageClass != oldCRD.Spec.DataStorageClass {
		logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
			Warningf("The Operator has refused the change on DataStorageClass from [%s] to NewValue[%s]",
				oldCRD.Spec.DataStorageClass, instance.Spec.DataStorageClass)
		instance.Spec.DataStorageClass = oldCRD.Spec.DataStorageClass
		return true
	}
	////Zookeeper Configuration change is forbidden
	//if instance.Spec.Zookeeper != oldCRD.Spec.Zookeeper {
	//	logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
	//		Warningf("The Operator has refused any change on Zookeeper")
	//	instance.Spec.Zookeeper = oldCRD.Spec.Zookeeper
	//	return true
	//}
	//if !reflect.DeepEqual(instance.Spec.Resources, oldCRD.Spec.Resources) {
	//	logrus.WithFields(logrus.Fields{"cluster": instance.Name}).
	//		Warningf("The Operator has refused the change on Resources from [%v] to NewValue[%v]",
	//			oldCRD.Spec.Resources, instance.Spec.Resources)
	//	instance.Spec.DataStorageClass = oldCRD.Spec.DataStorageClass
	//	return true
	//}
	return false
}

func (r *ReconcileClickHouseCluster) reconcileService(service *corev1.Service) error {
	var curService corev1.Service
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, &curService)
	// Object with such name does not exist or error happened
	if err != nil {
		if apierrors.IsNotFound(err) {
			logrus.WithFields(logrus.Fields{
				"service":   service.Name,
				"namespace": service.Namespace}).Info("Create Service")
			// Object with such name not found - create it
			return r.client.Create(context.TODO(), service)
		}
		return err
	}
	if curService.Spec.ClusterIP != "None" {
		service.Spec = curService.Spec
	}
	if reflect.DeepEqual(curService.Spec, service.Spec) {
		logrus.Debug("no need to update service")
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"service":   service.Name,
		"namespace": service.Namespace}).Info("Update Service")
	// spec.resourceVersion is required in order to update Service
	service.ResourceVersion = curService.ResourceVersion
	// spec.clusterIP field is immutable, need to use already assigned value
	// From https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service
	// Kubernetes assigns this Service an IP address (sometimes called the “cluster IP”), which is used by the Service proxies
	// See also https://kubernetes.io/docs/concepts/services-networking/service/#virtual-ips-and-service-proxies
	// You can specify your own cluster IP address as part of a Service creation request. To do this, set the .spec.clusterIP
	service.Spec.ClusterIP = curService.Spec.ClusterIP
	return r.client.Update(context.TODO(), service)
}

func (r *ReconcileClickHouseCluster) reconcileRoleBinding(roleBinding *rbacv1.RoleBinding) error {
	var curRoleBinding rbacv1.RoleBinding
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: roleBinding.Namespace, Name: roleBinding.Name}, &curRoleBinding)
	// Object with such name does not exist or error happened
	if err != nil && apierrors.IsNotFound(err) {
		// Object with such name not found - create it
		logrus.WithFields(logrus.Fields{
			"roleBinding": roleBinding.Name,
			"namespace":   roleBinding.Namespace}).Info("Create RoleBinding")
		return r.client.Create(context.TODO(), roleBinding)
	}
	return err
}

func (r *ReconcileClickHouseCluster) reconcileConfigMap(configMap *corev1.ConfigMap) error {
	var curConfigMap corev1.ConfigMap
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, &curConfigMap)
	// Object with such name does not exist or error happened
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object with such name not found - create it
			logrus.WithFields(logrus.Fields{
				"configmap": configMap.Name,
				"namespace": configMap.Namespace}).Info("Create ConfigMap")
			return r.client.Create(context.TODO(), configMap)
		}
		return err
	}

	if reflect.DeepEqual(curConfigMap.Data, configMap.Data) {
		logrus.Debug("no need to update configmap")
		return nil
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(fmt.Sprintf("%v", curConfigMap.Data), fmt.Sprintf("%v", configMap.Data), false)
	logrus.Debugf(dmp.DiffPrettyText(diffs))

	logrus.WithFields(logrus.Fields{
		"configmap": configMap.Name,
		"namespace": configMap.Namespace}).Info("Update ConfigMap")
	return r.client.Update(context.TODO(), configMap)
}

func (r *ReconcileClickHouseCluster) reconcileStatefulSet(clusterNew bool, statefulSet *appsv1.StatefulSet) error {
	// Check whether object with such name already exists in k8s
	var curStatefulSet appsv1.StatefulSet

	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: statefulSet.Namespace, Name: statefulSet.Name}, &curStatefulSet)
	// Object with such name does not exist or error happened
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object with such name not found - create it
			logrus.WithFields(logrus.Fields{
				"statefulset": statefulSet.Name,
				"namespace":   statefulSet.Namespace}).Info("Create StatefulSet")
			if !clusterNew {
				statefulSet.Annotations[ClusterHostsChange] = "true"
			}
			return r.client.Create(context.TODO(), statefulSet)
		}
		return err
	}

	//TODO: 更合适的比较
	if statefulSetsAreEqual(statefulSet, &curStatefulSet) {
		logrus.Debug("no need to update staefulset")
		return nil
	}

	if *statefulSet.Spec.Replicas > *curStatefulSet.Spec.Replicas {
		statefulSet.Annotations[ClusterHostsChange] = "true"
	}

	logrus.WithFields(logrus.Fields{
		"statefulSet": statefulSet.Name,
		"namespace":   statefulSet.Namespace}).Info("Update StatefulSet")
	return r.client.Update(context.TODO(), statefulSet)
}

func (r *ReconcileClickHouseCluster) DeletePVCs(cc *clickhousev1.ClickHouseCluster) error {
	logrus.Info("in delete pvcs function.")
	selector := map[string]string{
		ClusterLabelKey: cc.Name,
	}
	lpvc, err := r.listPVC(cc.Namespace, selector)
	if err != nil {
		logrus.Errorf("failed to get clickhouse's PVC: %v", err)
		return err
	}
	for _, pvc := range lpvc.Items {
		logrus.Info("", pvc.Name)
		err = r.client.Delete(context.TODO(), &pvc)
		if err != nil {
			logrus.WithFields(logrus.Fields{"PVC": pvc.Name, "Namespace": cc.Namespace}).Error("Error Deleting PVC")
		} else {
			logrus.WithFields(logrus.Fields{"PVC": pvc.Name, "Namespace": cc.Namespace}).Info("Delete PVC OK")
		}
	}
	return nil
}

func (r *ReconcileClickHouseCluster) listPVC(namespace string, selector map[string]string) (*corev1.PersistentVolumeClaimList, error) {
	opt := &client.ListOptions{Namespace: namespace, LabelSelector: labels.SelectorFromSet(selector)}
	o := &corev1.PersistentVolumeClaimList{}
	return o, r.client.List(context.TODO(), o, opt)
}

func (r *ReconcileClickHouseCluster) setDefaults(c *clickhousev1.ClickHouseCluster, config *config.DefaultConfig) bool {
	var changed = false
	if c.Status.Phase == "" {
		c.Status.Phase = ClusterPhaseInitial
		changed = true
	}
	if c.Spec.Image == "" {
		c.Spec.Image = config.DefaultClickhouseImage
		changed = true
	}
	if c.Spec.InitImage == "" {
		c.Spec.InitImage = config.DefaultClickhouseInitImage
		changed = true
	}
	if c.Spec.ShardsCount == 0 {
		c.Spec.ShardsCount = config.DefaultShardCount
		changed = true
	}
	if c.Spec.ReplicasCount == 0 {
		c.Spec.ReplicasCount = config.DefaultReplicasCount
		changed = true
	}
	//if c.Spec.Zookeeper == nil {
	//	zkConfig := *config.DefaultZookeeper
	//	zkConfig.Root = fmt.Sprintf("%s/%s/%s", zkConfig.Root, c.Namespace, c.Name)
	//	c.Spec.Zookeeper = &zkConfig
	//	changed = true
	//}
	if c.Spec.DataStorageClass != "" && c.Spec.DataCapacity == "" {
		c.Spec.DataCapacity = config.DefaultDataCapacity
	}
	if c.Spec.CustomSettings == "" {
		c.Spec.CustomSettings = "<yandex></yandex>"
		changed = true
	}
	if c.Spec.Users == "" {
		password := RandStringRunes(10)
		c.Spec.Users = fmt.Sprintf(defaultUserXML, password)
		changed = true
	}
	if c.Spec.Resources.Limits == (clickhousev1.CPUAndMem{}) {
		c.Spec.Resources.Limits = c.Spec.Resources.Requests
		changed = true
	}
	return changed
}

func (r *ReconcileClickHouseCluster) checkServiceMonitor(sm *monitoringv1.ServiceMonitor) error {
	if r.scheme.IsGroupRegistered("monitoring.coreos.com") != true {
		err := monitoringv1.AddToScheme(r.scheme)
		if err != nil {
			logrus.Error("cannot add monitorv1 to runtime scheme", err)
			return err
		}
	}

	oldSm := &monitoringv1.ServiceMonitor{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: sm.Namespace, Name: sm.Name}, oldSm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logrus.Info("no servicemonitor, will create one ")
			return r.client.Create(context.TODO(), sm)
		}
		logrus.WithField("error", err).Error("get servicemonitor error")
		return err
	}
	return nil
}

func (r *ReconcileClickHouseCluster) deleteZookeeperPath(cc *clickhousev1.ClickHouseCluster) error {
	if cc.Spec.Zookeeper == nil {
		return nil
	}

	hosts := make([]string, 0)
	for _, host := range cc.Spec.Zookeeper.Nodes {
		logrus.Infof("add zk node : %s/%s to delete list\n", host.Host, host.Port)
		hosts = append(hosts, fmt.Sprintf("%s:%d", host.Host, host.Port))
	}
	conn, _, err := zk.Connect(hosts, time.Second*10)
	if err != nil {
		return fmt.Errorf("connect zk err: %s", err.Error())
	}

	if cc.Spec.Zookeeper.Identity != "" {
		err = conn.AddAuth("digest", []byte(cc.Spec.Zookeeper.Identity))
		if err != nil {
			return err
		}
	}
	defer conn.Close()
	err = Deleteall(conn, cc.Spec.Zookeeper.Root)
	if err != nil {
		logrus.WithField("error", err).Errorf("failed to delete zookeeper path %s for clickhousecluster %s",
			cc.Spec.Zookeeper.Root, cc.Name)
		return err
	}
	return nil
}

func (r *ReconcileClickHouseCluster) checkInitZookeeperCfg(cc *clickhousev1.ClickHouseCluster) error {
	if cc.Spec.Zookeeper != nil && cc.Spec.Zookeeper.Nodes != nil && cc.Spec.Zookeeper.Nodes[0].Host != "" {
		return nil
	}

	if cc.Spec.ReplicasCount == 1 {
		return nil
	}

	return errors.New("must have zookeeper configuration when have replicas")

}

func (r *ReconcileClickHouseCluster) formatZookeeper(cc *clickhousev1.ClickHouseCluster) {
	if cc.Spec.Zookeeper == nil {
		return
	}
	// set clickhouse root node force
	cc.Spec.Zookeeper.Root = "/clickhouse/tables/" + cc.Namespace + "/" + cc.Name

	if cc.Spec.Zookeeper.OperationTimeoutMs == 0 {
		cc.Spec.Zookeeper.OperationTimeoutMs = 10000
	}
	if cc.Spec.Zookeeper.SessionTimeoutMs == 0 {
		cc.Spec.Zookeeper.SessionTimeoutMs = 30000
	}
}

// Deleteall recursively delete all nodes in zookeeper
func Deleteall(conn *zk.Conn, root string) error {
	logrus.Info("for root", root)
	cds, _, err := conn.Children(root)
	if err != nil {
		logrus.WithField("error", err).Error("fail to get children for ", root)
		return err
	}

	if cds != nil {
		for _, cd := range cds {
			_ = Deleteall(conn, root+"/"+cd)
		}
	}

	logrus.Info("deleting node ", root)
	err = conn.Delete(root, -1)
	if err != nil {
		logrus.WithField("error", err).Error("fail to get delete node ", root)
		return err
	}

	return nil

}
