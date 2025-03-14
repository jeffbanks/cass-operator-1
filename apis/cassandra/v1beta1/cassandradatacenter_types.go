// Copyright DataStax, Inc.
// Please see the included license file for details.

package v1beta1

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Jeffail/gabs"
	"github.com/k8ssandra/cass-operator/pkg/serverconfig"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// ClusterLabel is the operator's label for the cluster name
	ClusterLabel = "cassandra.datastax.com/cluster"

	// DatacenterLabel is the operator's label for the datacenter name
	DatacenterLabel = "cassandra.datastax.com/datacenter"

	// SeedNodeLabel is the operator's label for the seed node state
	SeedNodeLabel = "cassandra.datastax.com/seed-node"

	// RackLabel is the operator's label for the rack name
	RackLabel = "cassandra.datastax.com/rack"

	// RackLabel is the operator's label for the rack name
	CassOperatorProgressLabel = "cassandra.datastax.com/operator-progress"

	// PromMetricsLabel is a service label that can be selected for prometheus metrics scraping
	PromMetricsLabel = "cassandra.datastax.com/prom-metrics"

	// DatacenterAnnotation is the operator's annotation for the datacenter name
	DatacenterAnnotation = DatacenterLabel

	// ConfigHashAnnotation is the operator's annotation for the hash of the ConfigSecret
	ConfigHashAnnotation = "cassandra.datastax.com/config-hash"

	// SkipUserCreationAnnotation tells the operator to skip creating any Cassandra users
	// including the default superuser. This is for multi-dc deployments when adding a
	// DC to an existing cluster where the superuser has already been created.
	SkipUserCreationAnnotation = "cassandra.datastax.com/skip-user-creation"

	// DecommissionOnDeleteAnnotation allows to decommissioning of the Datacenter in a multi-DC cluster when the
	// CassandraDatacenter is deleted.
	DecommissionOnDeleteAnnotation = "cassandra.datastax.com/decommission-on-delete"

	// CassNodeState
	CassNodeState = "cassandra.datastax.com/node-state"

	// Progress states for status
	ProgressUpdating ProgressState = "Updating"
	ProgressReady    ProgressState = "Ready"

	// Default port numbers
	DefaultNativePort    = 9042
	DefaultInternodePort = 7000
)

// This type exists so there's no chance of pushing random strings to our progress status
type ProgressState string

type CassandraUser struct {
	SecretName string `json:"secretName"`
	Superuser  bool   `json:"superuser"`
}

// CassandraDatacenterSpec defines the desired state of a CassandraDatacenter
// +k8s:openapi-gen=true
// +kubebuilder:pruning:PreserveUnknownFields
// +kubebuilder:validation:XPreserveUnknownFields
type CassandraDatacenterSpec struct {
	// Desired number of Cassandra server nodes
	// +kubebuilder:validation:Minimum=1
	Size int32 `json:"size"`

	// Version string for config builder,
	// used to generate Cassandra server configuration
	// +kubebuilder:validation:Pattern=(6\.8\.\d+)|(3\.11\.\d+)|(4\.0\.\d+)
	ServerVersion string `json:"serverVersion"`

	// Cassandra server image name. Use of ImageConfig to match ServerVersion is recommended instead of this value.
	// This value will override anything set in the ImageConfig matching the ServerVersion
	// More info: https://kubernetes.io/docs/concepts/containers/images
	ServerImage string `json:"serverImage,omitempty"`

	// Server type: "cassandra" or "dse"
	// +kubebuilder:validation:Enum=cassandra;dse
	ServerType string `json:"serverType"`

	// Does the Server Docker image run as the Cassandra user? Defaults to true
	DockerImageRunsAsCassandra *bool `json:"dockerImageRunsAsCassandra,omitempty"`

	// Config for the server, in YAML format
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:XPreserveUnknownFields
	Config json.RawMessage `json:"config,omitempty"`

	// ConfigSecret is the name of a secret that contains configuration for Cassandra. The
	// secret is expected to have a property named config whose value should be a JSON
	// formatted string that should look like this:
	//
	//    config: |-
	//      {
	//        "cassandra-yaml": {
	//          "read_request_timeout_in_ms": 10000
	//        },
	//        "jmv-options": {
	//          "max_heap_size": 1024M
	//        }
	//      }
	//
	// ConfigSecret is mutually exclusive with Config. ConfigSecret takes precedence and
	// will be used exclusively if both properties are set. The operator sets a watch such
	// that an update to the secret will trigger an update of the StatefulSets.
	ConfigSecret string `json:"configSecret,omitempty"`

	// Config for the Management API certificates
	ManagementApiAuth ManagementApiAuthConfig `json:"managementApiAuth,omitempty"`

	//NodeAffinityLabels to pin the Datacenter, using node affinity
	NodeAffinityLabels map[string]string `json:"nodeAffinityLabels,omitempty"`

	// Kubernetes resource requests and limits, per pod
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Kubernetes resource requests and limits per system logger container.
	SystemLoggerResources corev1.ResourceRequirements `json:"systemLoggerResources,omitempty"`

	// Kubernetes resource requests and limits per server config initialization container.
	ConfigBuilderResources corev1.ResourceRequirements `json:"configBuilderResources,omitempty"`

	// A list of the named racks in the datacenter, representing independent failure domains. The
	// number of racks should match the replication factor in the keyspaces you plan to create, and
	// the number of racks cannot easily be changed once a datacenter is deployed.
	Racks []Rack `json:"racks,omitempty"`

	// Describes the persistent storage request of each server node
	StorageConfig StorageConfig `json:"storageConfig"`

	// A list of pod names that need to be replaced.
	ReplaceNodes []string `json:"replaceNodes,omitempty"`

	// The name by which CQL clients and instances will know the cluster. If the same
	// cluster name is shared by multiple Datacenters in the same Kubernetes namespace,
	// they will join together in a multi-datacenter cluster.
	// +kubebuilder:validation:MinLength=2
	ClusterName string `json:"clusterName"`

	// A stopped CassandraDatacenter will have no running server pods, like using "stop" with
	// traditional System V init scripts. Other Kubernetes resources will be left intact, and volumes
	// will re-attach when the CassandraDatacenter workload is resumed.
	Stopped bool `json:"stopped,omitempty"`

	// Container image for the config builder init container. Overrides value from ImageConfig ConfigBuilderImage
	ConfigBuilderImage string `json:"configBuilderImage,omitempty"`

	// Indicates that configuration and container image changes should only be pushed to
	// the first rack of the datacenter
	CanaryUpgrade bool `json:"canaryUpgrade,omitempty"`

	// The number of nodes that will be updated when CanaryUpgrade is true. Note that the value is
	// either 0 or greater than the rack size, then all nodes in the rack will get updated.
	CanaryUpgradeCount int32 `json:"canaryUpgradeCount,omitempty"`

	// Turning this option on allows multiple server pods to be created on a k8s worker node.
	// By default the operator creates just one server pod per k8s worker node using k8s
	// podAntiAffinity and requiredDuringSchedulingIgnoredDuringExecution.
	AllowMultipleNodesPerWorker bool `json:"allowMultipleNodesPerWorker,omitempty"`

	// This secret defines the username and password for the Cassandra server superuser.
	// If it is omitted, we will generate a secret instead.
	SuperuserSecretName string `json:"superuserSecretName,omitempty"`

	// The k8s service account to use for the server pods
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// Whether to do a rolling restart at the next opportunity. The operator will set this back
	// to false once the restart is in progress.
	RollingRestartRequested bool `json:"rollingRestartRequested,omitempty"`

	// A map of label keys and values to restrict Cassandra node scheduling to k8s workers
	// with matchiing labels.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Rack names in this list are set to the latest StatefulSet configuration
	// even if Cassandra nodes are down. Use this to recover from an upgrade that couldn't
	// roll out.
	ForceUpgradeRacks []string `json:"forceUpgradeRacks,omitempty"`

	DseWorkloads *DseWorkloads `json:"dseWorkloads,omitempty"`

	// PodTemplate provides customisation options (labels, annotations, affinity rules, resource requests, and so on) for the cassandra pods
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`

	// Cassandra users to bootstrap
	Users []CassandraUser `json:"users,omitempty"`

	Networking *NetworkingConfig `json:"networking,omitempty"`

	AdditionalSeeds []string `json:"additionalSeeds,omitempty"`

	// Configuration for disabling the simple log tailing sidecar container. Our default is to have it enabled.
	DisableSystemLoggerSidecar bool `json:"disableSystemLoggerSidecar,omitempty"`

	// Container image for the log tailing sidecar container. Overrides value from ImageConfig SystemLoggerImage
	SystemLoggerImage string `json:"systemLoggerImage,omitempty"`

	// AdditionalServiceConfig allows to define additional parameters that are included in the created Services. Note, user can override values set by cass-operator and doing so could break cass-operator functionality.
	// Avoid label "cass-operator" and anything that starts with "cassandra.datastax.com/"
	AdditionalServiceConfig ServiceConfig `json:"additionalServiceConfig,omitempty"`

	// Tolerations applied to the Cassandra pod. Note that these cannot be overridden with PodTemplateSpec.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Additional Labels allows to define additional labels that will be included in all objects created by the operator. Note, user can override values set by default from the cass-operator and doing so could break cass-operator functionality.
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`
}

type NetworkingConfig struct {
	NodePort    *NodePortConfig `json:"nodePort,omitempty"`
	HostNetwork bool            `json:"hostNetwork,omitempty"`
}

type NodePortConfig struct {
	Native       int `json:"native,omitempty"`
	NativeSSL    int `json:"nativeSSL,omitempty"`
	Internode    int `json:"internode,omitempty"`
	InternodeSSL int `json:"internodeSSL,omitempty"`
}

// Is the NodePort service enabled?
func (dc *CassandraDatacenter) IsNodePortEnabled() bool {
	return dc.Spec.Networking != nil && dc.Spec.Networking.NodePort != nil
}

func (dc *CassandraDatacenter) IsHostNetworkEnabled() bool {
	networking := dc.Spec.Networking
	return networking != nil && networking.HostNetwork
}

type DseWorkloads struct {
	AnalyticsEnabled bool `json:"analyticsEnabled,omitempty"`
	GraphEnabled     bool `json:"graphEnabled,omitempty"`
	SearchEnabled    bool `json:"searchEnabled,omitempty"`
}

// StorageConfig defines additional storage configurations
type AdditionalVolumes struct {
	// Mount path into cassandra container
	MountPath string `json:"mountPath"`
	// Name of the pvc
	// +kubebuilder:validation:Pattern=[a-z0-9]([-a-z0-9]*[a-z0-9])?
	Name string `json:"name"`
	// Persistent volume claim spec
	PVCSpec corev1.PersistentVolumeClaimSpec `json:"pvcSpec"`
}

type AdditionalVolumesSlice []AdditionalVolumes

type StorageConfig struct {
	CassandraDataVolumeClaimSpec *corev1.PersistentVolumeClaimSpec `json:"cassandraDataVolumeClaimSpec,omitempty"`
	AdditionalVolumes            AdditionalVolumesSlice            `json:"additionalVolumes,omitempty"`
}

// GetRacks is a getter for the Rack slice in the spec
// It ensures there is always at least one rack
func (dc *CassandraDatacenter) GetRacks() []Rack {
	if len(dc.Spec.Racks) >= 1 {
		return dc.Spec.Racks
	}

	return []Rack{{
		Name: "default",
	}}
}

// ServiceConfig defines additional service configurations.
type ServiceConfig struct {
	DatacenterService     ServiceConfigAdditions `json:"dcService,omitempty"`
	SeedService           ServiceConfigAdditions `json:"seedService,omitempty"`
	AllPodsService        ServiceConfigAdditions `json:"allpodsService,omitempty"`
	AdditionalSeedService ServiceConfigAdditions `json:"additionalSeedService,omitempty"`
	NodePortService       ServiceConfigAdditions `json:"nodePortService,omitempty"`
}

// ServiceConfigAdditions exposes additional options for each service
type ServiceConfigAdditions struct {
	Labels      map[string]string `json:"additionalLabels,omitempty"`
	Annotations map[string]string `json:"additionalAnnotations,omitempty"`
}

// Rack ...
type Rack struct {
	// The rack name
	// +kubebuilder:validation:MinLength=2
	Name string `json:"name"`

	// Deprecated. Use nodeAffinityLabels instead. Zone name to pin the rack, using node affinity
	Zone string `json:"zone,omitempty"`

	//NodeAffinityLabels to pin the rack, using node affinity
	NodeAffinityLabels map[string]string `json:"nodeAffinityLabels,omitempty"`
}

type CassandraNodeStatus struct {
	HostID string `json:"hostID,omitempty"`
}

type CassandraStatusMap map[string]CassandraNodeStatus

type DatacenterConditionType string

const (
	DatacenterReady          DatacenterConditionType = "Ready"
	DatacenterInitialized    DatacenterConditionType = "Initialized"
	DatacenterReplacingNodes DatacenterConditionType = "ReplacingNodes"
	DatacenterScalingUp      DatacenterConditionType = "ScalingUp"
	DatacenterScalingDown    DatacenterConditionType = "ScalingDown"
	DatacenterUpdating       DatacenterConditionType = "Updating"
	DatacenterStopped        DatacenterConditionType = "Stopped"
	DatacenterResuming       DatacenterConditionType = "Resuming"
	DatacenterRollingRestart DatacenterConditionType = "RollingRestart"
	DatacenterValid          DatacenterConditionType = "Valid"
	DatacenterDecommission   DatacenterConditionType = "Decommission"
)

type DatacenterCondition struct {
	Type               DatacenterConditionType `json:"type"`
	Status             corev1.ConditionStatus  `json:"status"`
	Reason             string                  `json:"reason"`
	Message            string                  `json:"message"`
	LastTransitionTime metav1.Time             `json:"lastTransitionTime,omitempty"`
}

func NewDatacenterCondition(conditionType DatacenterConditionType, status corev1.ConditionStatus) *DatacenterCondition {
	return &DatacenterCondition{
		Type:    conditionType,
		Status:  status,
		Reason:  "",
		Message: "",
	}
}

func NewDatacenterConditionWithReason(conditionType DatacenterConditionType, status corev1.ConditionStatus, reason string, message string) *DatacenterCondition {
	return &DatacenterCondition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

// CassandraDatacenterStatus defines the observed state of CassandraDatacenter
// +k8s:openapi-gen=true
type CassandraDatacenterStatus struct {
	Conditions []DatacenterCondition `json:"conditions,omitempty"`

	// Deprecated. Use usersUpserted instead. The timestamp at
	// which CQL superuser credentials were last upserted to the
	// management API
	// +optional
	SuperUserUpserted metav1.Time `json:"superUserUpserted,omitempty"`

	// The timestamp at which managed cassandra users' credentials
	// were last upserted to the management API
	// +optional
	UsersUpserted metav1.Time `json:"usersUpserted,omitempty"`

	// The timestamp when the operator last started a Server node
	// with the management API
	// +optional
	LastServerNodeStarted metav1.Time `json:"lastServerNodeStarted,omitempty"`

	// Last known progress state of the Cassandra Operator
	// +optional
	CassandraOperatorProgress ProgressState `json:"cassandraOperatorProgress,omitempty"`

	// +optional
	LastRollingRestart metav1.Time `json:"lastRollingRestart,omitempty"`

	// +optional
	NodeStatuses CassandraStatusMap `json:"nodeStatuses"`

	// +optional
	NodeReplacements []string `json:"nodeReplacements"`

	// +optional
	QuietPeriod metav1.Time `json:"quietPeriod,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// TrackedTasks tracks the tasks for completion that were created by the cass-operator
	// +optional
	TrackedTasks []corev1.ObjectReference `json:"trackedTasks,omitempty"`
}

// CassandraDatacenter is the Schema for the cassandradatacenters API
// +k8s:openapi-gen=true
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:path=cassandradatacenters,scope=Namespaced,shortName=cassdc;cassdcs
type CassandraDatacenter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CassandraDatacenterSpec   `json:"spec,omitempty"`
	Status CassandraDatacenterStatus `json:"status,omitempty"`
}

type ManagementApiAuthManualConfig struct {
	ClientSecretName string `json:"clientSecretName"`
	ServerSecretName string `json:"serverSecretName"`
	// +optional
	SkipSecretValidation bool `json:"skipSecretValidation,omitempty"`
}

type ManagementApiAuthInsecureConfig struct {
}

type ManagementApiAuthConfig struct {
	Insecure *ManagementApiAuthInsecureConfig `json:"insecure,omitempty"`
	Manual   *ManagementApiAuthManualConfig   `json:"manual,omitempty"`
	// other strategy configs (e.g. Cert Manager) go here
}

//+kubebuilder:object:root=true

// CassandraDatacenterList contains a list of CassandraDatacenter
type CassandraDatacenterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CassandraDatacenter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CassandraDatacenter{}, &CassandraDatacenterList{})
}

func (dc *CassandraDatacenter) GetConfigBuilderImage() string {
	return dc.Spec.ConfigBuilderImage
}

// GetServerImage produces a fully qualified container image to pull
// based on either the version, or an explicitly specified image
//
// In the event that no valid image could be retrieved from the specified version,
// an error is returned.
func (dc *CassandraDatacenter) GetServerImage() string {
	return dc.Spec.ServerImage
}

// GetRackLabels ...
func (dc *CassandraDatacenter) GetRackLabels(rackName string) map[string]string {
	labels := dc.GetDatacenterLabels()
	labels[RackLabel] = rackName
	return labels
}

func (status *CassandraDatacenterStatus) GetConditionStatus(conditionType DatacenterConditionType) corev1.ConditionStatus {
	for _, condition := range status.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return corev1.ConditionUnknown
}

func (status *CassandraDatacenterStatus) AddTaskToTrack(objectMeta metav1.ObjectMeta) {
	if status.TrackedTasks == nil {
		status.TrackedTasks = make([]corev1.ObjectReference, 0, 1)
	}

	status.TrackedTasks = append(status.TrackedTasks, corev1.ObjectReference{
		Name:      objectMeta.Name,
		Namespace: objectMeta.Namespace,
	})
}

func (status *CassandraDatacenterStatus) RemoveTrackedTask(objectMeta metav1.ObjectMeta) {
	for index, task := range status.TrackedTasks {
		if task.Name == objectMeta.Name && task.Namespace == objectMeta.Namespace {
			status.TrackedTasks = append(status.TrackedTasks[:index], status.TrackedTasks[index+1:]...)
		}
	}
}

func (dc *CassandraDatacenter) GetConditionStatus(conditionType DatacenterConditionType) corev1.ConditionStatus {
	return (&dc.Status).GetConditionStatus(conditionType)
}

func (dc *CassandraDatacenter) GetCondition(conditionType DatacenterConditionType) (DatacenterCondition, bool) {
	for _, condition := range dc.Status.Conditions {
		if condition.Type == conditionType {
			return condition, true
		}
	}

	return DatacenterCondition{}, false
}

func (status *CassandraDatacenterStatus) SetCondition(condition DatacenterCondition) {
	conditions := status.Conditions
	added := false
	for i := range status.Conditions {
		if status.Conditions[i].Type == condition.Type {
			status.Conditions[i] = condition
			added = true
		}
	}

	if !added {
		conditions = append(conditions, condition)
	}

	status.Conditions = conditions
}

func (dc *CassandraDatacenter) SetCondition(condition DatacenterCondition) {
	(&dc.Status).SetCondition(condition)
}

// GetDatacenterLabels ...
func (dc *CassandraDatacenter) GetDatacenterLabels() map[string]string {
	labels := dc.GetClusterLabels()
	labels[DatacenterLabel] = dc.Name
	return labels
}

// GetClusterLabels returns a new map with the cluster label key and cluster name value
func (dc *CassandraDatacenter) GetClusterLabels() map[string]string {
	return map[string]string{
		ClusterLabel: dc.Spec.ClusterName,
	}
}

// apimachinery validation does not expose this, copied here
const dns1035LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

func CleanupForKubernetes(input string) string {
	if len(validation.IsDNS1035Label(input)) > 0 {
		r := regexp.MustCompile(dns1035LabelFmt)

		// Invalid domain name, Kubernetes will reject this. Try to modify it to a suitable string
		input = strings.ToLower(input)
		input = strings.ReplaceAll(input, "_", "-")
		validParts := r.FindAllString(input, -1)
		return strings.Join(validParts, "")
	}

	return input
}

func (dc *CassandraDatacenter) GetSeedServiceName() string {
	return CleanupForKubernetes(dc.Spec.ClusterName) + "-seed-service"
}

func (dc *CassandraDatacenter) GetAdditionalSeedsServiceName() string {
	return CleanupForKubernetes(dc.Spec.ClusterName) + "-" + dc.Name + "-additional-seed-service"
}

func (dc *CassandraDatacenter) GetAllPodsServiceName() string {
	return CleanupForKubernetes(dc.Spec.ClusterName) + "-" + dc.Name + "-all-pods-service"
}

func (dc *CassandraDatacenter) GetDatacenterServiceName() string {
	return CleanupForKubernetes(dc.Spec.ClusterName) + "-" + dc.Name + "-service"
}

func (dc *CassandraDatacenter) GetNodePortServiceName() string {
	return CleanupForKubernetes(dc.Spec.ClusterName) + "-" + dc.Name + "-node-port-service"
}

func (dc *CassandraDatacenter) ShouldGenerateSuperuserSecret() bool {
	return len(dc.Spec.SuperuserSecretName) == 0
}

func (dc *CassandraDatacenter) GetSuperuserSecretNamespacedName() types.NamespacedName {
	name := CleanupForKubernetes(dc.Spec.ClusterName) + "-superuser"
	namespace := dc.ObjectMeta.Namespace
	if len(dc.Spec.SuperuserSecretName) > 0 {
		name = dc.Spec.SuperuserSecretName
	}

	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

// GetConfigAsJSON gets a JSON-encoded string suitable for passing to configBuilder
func (dc *CassandraDatacenter) GetConfigAsJSON(config []byte) (string, error) {

	// We use the cluster seed-service name here for the seed list as it will
	// resolve to the seed nodes. This obviates the need to update the
	// cassandra.yaml whenever the seed nodes change.
	seeds := []string{dc.GetSeedServiceName(), dc.GetAdditionalSeedsServiceName()}

	graphEnabled := 0
	solrEnabled := 0
	sparkEnabled := 0

	if dc.Spec.ServerType == "dse" && dc.Spec.DseWorkloads != nil {
		if dc.Spec.DseWorkloads.AnalyticsEnabled {
			sparkEnabled = 1
		}
		if dc.Spec.DseWorkloads.GraphEnabled {
			graphEnabled = 1
		}
		if dc.Spec.DseWorkloads.SearchEnabled {
			solrEnabled = 1
		}
	}

	native := 0
	nativeSSL := 0
	internode := 0
	internodeSSL := 0
	if dc.IsNodePortEnabled() {
		native = dc.Spec.Networking.NodePort.Native
		nativeSSL = dc.Spec.Networking.NodePort.NativeSSL
		internode = dc.Spec.Networking.NodePort.Internode
		internodeSSL = dc.Spec.Networking.NodePort.InternodeSSL
	}

	modelValues := serverconfig.GetModelValues(
		seeds,
		dc.Spec.ClusterName,
		dc.Name,
		graphEnabled,
		solrEnabled,
		sparkEnabled,
		native,
		nativeSSL,
		internode,
		internodeSSL)

	var modelBytes []byte

	modelBytes, err := json.Marshal(modelValues)
	if err != nil {
		return "", err
	}

	// Combine the model values with the user-specified values

	modelParsed, err := gabs.ParseJSON([]byte(modelBytes))
	if err != nil {
		return "", errors.Wrap(err, "Model information for CassandraDatacenter resource was not properly configured")
	}

	if config != nil {
		configParsed, err := gabs.ParseJSON(config)
		if err != nil {
			return "", errors.Wrap(err, "Error parsing Spec.Config for CassandraDatacenter resource")
		}

		if err := modelParsed.Merge(configParsed); err != nil {
			return "", errors.Wrap(err, "Error merging Spec.Config for CassandraDatacenter resource")
		}
	}

	return modelParsed.String(), nil
}

// Gets the defined CQL port for NodePort.
// 0 will be returned if NodePort is not configured.
// The SSL port will be returned if it is defined,
// otherwise the normal CQL port will be used.
func (dc *CassandraDatacenter) GetNodePortNativePort() int {
	if !dc.IsNodePortEnabled() {
		return 0
	}

	if dc.Spec.Networking.NodePort.NativeSSL != 0 {
		return dc.Spec.Networking.NodePort.NativeSSL
	} else if dc.Spec.Networking.NodePort.Native != 0 {
		return dc.Spec.Networking.NodePort.Native
	} else {
		return DefaultNativePort
	}
}

// Gets the defined internode/broadcast port for NodePort.
// 0 will be returned if NodePort is not configured.
// The SSL port will be returned if it is defined,
// otherwise the normal internode port will be used.
func (dc *CassandraDatacenter) GetNodePortInternodePort() int {
	if !dc.IsNodePortEnabled() {
		return 0
	}

	if dc.Spec.Networking.NodePort.InternodeSSL != 0 {
		return dc.Spec.Networking.NodePort.InternodeSSL
	} else if dc.Spec.Networking.NodePort.Internode != 0 {
		return dc.Spec.Networking.NodePort.Internode
	} else {
		return DefaultInternodePort
	}
}

func namedPort(name string, port int) corev1.ContainerPort {
	return corev1.ContainerPort{Name: name, ContainerPort: int32(port)}
}

// GetContainerPorts will return the container ports for the pods in a statefulset based on the provided config
func (dc *CassandraDatacenter) GetContainerPorts() ([]corev1.ContainerPort, error) {

	nativePort := DefaultNativePort
	internodePort := DefaultInternodePort

	// Note: Port Names cannot be more than 15 characters

	ports := []corev1.ContainerPort{
		namedPort("native", nativePort),
		namedPort("tls-native", 9142),
		namedPort("internode", internodePort),
		namedPort("tls-internode", 7001),
		namedPort("jmx", 7199),
		namedPort("mgmt-api-http", 8080),
		namedPort("prometheus", 9103),
		namedPort("thrift", 9160),
	}

	if dc.Spec.ServerType == "dse" {
		ports = append(
			ports,
			namedPort("internode-msg", 8609),
		)
	}

	if dc.Spec.DseWorkloads != nil {
		if dc.Spec.DseWorkloads.AnalyticsEnabled {
			ports = append(
				ports,
				namedPort("spark-app-4040", 4040),
				namedPort("spark-app-4041", 4041),
				namedPort("spark-app-4042", 4042),
				namedPort("spark-app-4043", 4043),
				namedPort("spark-app-4044", 4044),
				namedPort("spark-app-4045", 4045),
				namedPort("spark-app-4046", 4046),
				namedPort("spark-app-4047", 4047),
				namedPort("spark-app-4048", 4048),
				namedPort("spark-app-4049", 4049),
				namedPort("spark-app-4050", 4050),
				namedPort("dsefs-public", 5598),
				namedPort("dsefs-internode", 5599),
				namedPort("spark-internode", 7077),
				namedPort("spark-master", 7080),
				namedPort("spark-worker", 7081),
				namedPort("jobserver", 8090),
				namedPort("always-on-sql", 9077),
				namedPort("jobserver-jmx", 9999),
				namedPort("sql-thrift", 10000),
				namedPort("spark-history", 18080),
			)
		}

		if dc.Spec.DseWorkloads.GraphEnabled {
			ports = append(
				ports,
				namedPort("gremlin", 8182),
			)
		}

		if dc.Spec.DseWorkloads.SearchEnabled {
			ports = append(
				ports,
				namedPort("solr", 8983),
			)
		}
	}

	return ports, nil
}

func (dc *CassandraDatacenter) FullQueryEnabled() (bool, error) {
	// TODO Cleanup to more common processing after ModelValues is moved to apis
	if dc.Spec.Config != nil {
		var dcConfig map[string]interface{}
		if err := json.Unmarshal(dc.Spec.Config, &dcConfig); err != nil {
			return false, err
		}
		casYaml, found := dcConfig["cassandra-yaml"]
		if !found {
			return false, nil
		}
		casYamlMap, ok := casYaml.(map[string]interface{})
		if !ok {
			err := fmt.Errorf("failed to parse cassandra-yaml")
			return false, err
		}
		if _, found := casYamlMap["full_query_logging_options"]; found {
			return true, nil
		}
	}

	return false, nil
}

func (dc *CassandraDatacenter) DeploymentSupportsFQL() bool {
	serverMajorVersion, err := strconv.ParseInt(strings.Split(dc.Spec.ServerVersion, ".")[0], 10, 8)
	if err != nil {
		return false
	}
	if serverMajorVersion < 4 || dc.Spec.ServerType != "cassandra" {
		// DSE does not support FQL
		return false
	}

	return true
}

func SplitRacks(nodeCount, rackCount int) []int {
	nodesPerRack, extraNodes := nodeCount/rackCount, nodeCount%rackCount

	var topology []int

	for rackIdx := 0; rackIdx < rackCount; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}
		topology = append(topology, nodesForThisRack)
	}

	return topology
}
