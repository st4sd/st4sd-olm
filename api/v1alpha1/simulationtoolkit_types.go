/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"crypto/md5"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	STATUS_UNKNOWN    = ""
	STATUS_PAUSED     = "Paused"
	STATUS_FAILED     = "Failed"
	STATUS_SUCCESSFUL = "Successful"
	STATUS_UPDATING   = "Updating"

	INTERPOLATE_CLUSTER_INGRESS = "${CLUSTER_INGRESS}"
	REASON_UPDATE_SOON          = "WillStartUpdatingSoon"
)

// VV: This value is auto-updated on build, like so (it cannot be a const):
//
//	go build -ldflags="-X 'github.com/st4sd/st4sd-olm/api/v1alpha1.OPERATOR_VERSION=$VERSION'" \
//	         -a -o manager main.go
var OPERATOR_VERSION = "dev"

type SimulationToolkitSpecSetup struct {
	// Name of the Persistent Volume Claim (PVC) to store the virtual experiment instances.
	// This PVC must already exist. It must also support mounting the PVC in multiple pods
	// (ReadWriteMany) in Filesystem mode.
	// This PVC must already exist.
	PVCInstances string `json:"pvcInstances,omitempty"`

	// Name of the Persistent Volume Claim (PVC) to hold the contents of the Datastore.
	// This PVC must already exist, it must support mounting the PVC (ReadWrite) in Filesystem mode
	// (preferably switch on ReadWriteMany access when creating this PVC).
	// This PVC must already exist.
	PVCDatastore string `json:"pvcDatastore,omitempty"`

	// Name of the PVC to hold metadata about the experiment catalog of
	// the Consumable Computing REST-API.
	// This PVC must already exist.
	PVCRuntimeService string `json:"pvcRuntimeService,omitempty"`

	// The name of the deployment. This is a short identifier with no spaces or '/' characters.
	// ST4SD uses it to generate unique identifiers for all virtual experiments
	// that this deployment executes.
	DatastoreIdentifier string `json:"datastoreIdentifier,omitempty"`

	// Domain to use in the Route object of the ST4SD OAuthProxy side-car container.
	// Consider using the format: ${clusterHumanReadableUID}.${CLUSTER_INGRESS}.
	// You can find the ${CLUSTER_INGRESS} of your OpenShift cluster via
	//
	// oc get ingress.v1.config.openshift.io cluster -o=jsonpath='{.spec.domain}'
	RouteDomain string `json:"routeDomain,omitempty"`

	// (Optional) Name of Secret that contains the keys username and password to use for setting up
	// the "admin" account of the MongoDB instance for the Datastore. The value of the username field
	// must be "admin". The value of the password should be a valid MongoDB password.
	// If empty, the operator will auto-generate the credentials of the MongoDB admin and store
	// them in a new Kubernetes secret.
	SecretDSMongoUserPass string `json:"secretDSMongoUserPass,omitempty"`

	// (Optional) Name of Secret that contains the keys
	// ENDPOINT (required), BUCKET (required), S3_ACCESS_KEY_ID (optional),
	// S3_SECRET_ACCESS_KEY (optional),  S3_REGION (optional).
	// When set configures the st4sd-runtime-service to switch on its Internal Experiment feature
	// which in turn enables users of the st4sd-registry-ui web-app to create workflows
	// in an interactive canvas. The st4sd-runtime-service will store the DSL 2.0 workflow definitions
	// in the referenced S3 bucket with the prefix "experiments/".
	SecretS3InternalExperiments string `json:"secretS3InternalExperiments,omitempty"`

	// (Optional) Name of Secret that contains the keys
	// ENDPOINT (required), BUCKET (required), S3_ACCESS_KEY_ID (optional),
	// S3_SECRET_ACCESS_KEY (optional),  S3_REGION (optional).
	// When set configures the st4sd-runtime-service to switch on its Graph Library feature
	// which in turn enables users of the st4sd-registry-ui web-app to use Graph templates
	// that are stored in the Graph Library when creating workflows in an interactive canvas.
	// The st4sd-runtime-service will store the Graph templates in the referenced S3 bucket
	// with the prefix "library/".
	SecretS3GraphLibrary string `json:"secretS3GraphLibrary,omitempty"`
}

// SimulationToolkitSpec defines the desired state of SimulationToolkit
type SimulationToolkitSpec struct {
	// Configuration options for the deployment of the Simulation Toolkit for Scientific Discovery
	// (ST4SD). The operator will use this information to instantiate the ST4SD helm chart
	// (https://github.com/st4sd/st4sd-deployment).
	Setup SimulationToolkitSpecSetup `json:"setup,omitempty"`

	// If true, the operator will not attempt to update/install ST4SD. Default is "false".
	Paused bool `json:"paused,omitempty"`
}

type SimulationToolkitVersion struct {
	// VersionID consists of the a / separated array of strings. The strings are (in this order)
	//  st4sd-olm (this operator) version, Helm Chart (in st4sd-deployment) version,
	//  ST4SD-Cloud version (library version in helm chart).
	VersionID string `json:"versionID,omitempty"`

	// The version of ST4SD-Cloud (i.e. the library version in the st4sd-deployment helm-chart)
	VersionST4SDCloud string `json:"versionST4SDCloud,omitempty"`
}

type SimulationToolkitStatusCondition struct {
	// The last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The last time this condition was updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
	// The reason for the condition’s last transition.
	Reason string `json:"reason,omitempty"`
	// Status of the condition, one of Paused, Updating, Failed, Successful, Unknown
	Status string `json:"status,omitempty"`

	SimulationToolkitVersion `json:",inline"`
}

// SimulationToolkitStatus defines the observed state of SimulationToolkit
type SimulationToolkitStatus struct {

	// Status of the condition, one of Paused, Updating, Failed, Successful, Unknown or empty (i.e. Unknown)
	Phase      string                             `json:"phase,omitempty"`
	Conditions []SimulationToolkitStatusCondition `json:"conditions,omitempty"`

	SimulationToolkitVersion `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=simulationtoolkits,shortName=st4sd
// +kubebuilder:printcolumn:name="age",type="string",JSONPath=".metadata.creationTimestamp",description="Age of the workflow instance"
// +kubebuilder:printcolumn:name="status",type="string",JSONPath=".status.phase",description="Latest status of deployment"
// +kubebuilder:printcolumn:name="versionID",type="string",JSONPath=".status.versionID",description="VersionID consists of a separated by '/' array of strings. The strings are (in this order) st4sd-olm-deploy (this operator) version, Helm Chart version, ST4SD version."
// +kubebuilder:printcolumn:name="versionST4SDCloud",type="string",JSONPath=".status.versionST4SDCloud",description="The version of ST4SD-Cloud"
// SimulationToolkit contains setup instructions to deploy the Simulation Toolkit for Scientific Discovery
// (ST4SD).
type SimulationToolkit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SimulationToolkitSpec   `json:"spec,omitempty"`
	Status SimulationToolkitStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SimulationToolkitList contains a list of SimulationToolkit
type SimulationToolkitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SimulationToolkit `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SimulationToolkit{}, &SimulationToolkitList{})
}

func (c *SimulationToolkitSpecSetup) Hash() string {
	contents, _ := json.Marshal(*c)
	return fmt.Sprintf("%x", md5.Sum(contents))
}
