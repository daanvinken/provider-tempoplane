/*
Copyright 2022 The Crossplane Authors.

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
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// EntityWorkflowObservation represents the observed state of an EntityWorkflow.
type EntityWorkflowObservation struct {
	// WorkflowID is the unique identifier of the Temporal workflow.
	WorkflowID string `json:"workflowID,omitempty"`

	// RunID is the unique identifier for the last known run of the Temporal workflow.
	RunID string `json:"runID,omitempty"`

	// Status indicates the current state of the workflow, such as running, completed, or failed.
	Status string `json:"status,omitempty"`

	// Message provides additional details or error messages related to the workflow status.
	Message string `json:"message,omitempty"`

	// LastUpdated is the timestamp of the last status update.
	LastUpdated int64 `json:"lastUpdated,omitempty"`
}

// EntityWorkflowParameters defines the input parameters required for the workflow operations on EntityWorkflow.
type EntityWorkflowParameters struct {
	// EntityInput captures the input data required for CRUD operations.
	EntityInput EntityInput `json:"entityInput"`
}

// EntityWorkflowSpec defines the desired state of an EntityWorkflow, including the EntityInput for CRUD operations.
type EntityWorkflowSpec struct {
	// ResourceSpec includes standard resource configuration like provider configuration references.
	xpv1.ResourceSpec `json:",inline"`

	// ForProvider contains the provider-specific input parameters for the workflow.
	ForProvider EntityWorkflowParameters `json:"forProvider"`
}

// EntityWorkflowStatus represents the observed state of an EntityWorkflow, including details about the Temporal workflow execution.
type EntityWorkflowStatus struct {
	// ResourceStatus includes common fields for tracking the status of external resources.
	xpv1.ResourceStatus `json:",inline"`

	// WorkflowID `json:",inline"`
	WorkflowID string `json:"workflowID,omitempty"`

	// AtProvider contains the provider-specific observed state, such as workflow IDs and status.
	AtProvider EntityWorkflowObservation `json:"atProvider,omitempty"`
}

// EntityInput represents the input data required for CRUD operations on a MyType.
type EntityInput struct {
	// EntityID is the unique identifier for the entity in the workflow.
	EntityID string `json:"entityID"`

	// Kind specifies the kind of entity (e.g., MyType, ResourceType).
	Kind string `json:"kind"`

	// APIVersion specifies the API version of the entity.
	APIVersion string `json:"apiVersion"`

	// Data contains the main payload for the entity.
	Data string `json:"data"`

	// RequesterID identifies the entity's requester.
	RequesterID string `json:"requesterID"`

	// DC specifies the data center for deployment or location context.
	DC string `json:"dc"`

	// Env specifies the environment (e.g., production, development).
	Env string `json:"env"`

	// Timestamp is the Unix timestamp of when the request was made.
	Timestamp int64 `json:"timestamp"`

	// CorrelationID is used for tracking and correlating requests across services.
	CorrelationID string `json:"correlationID"`
}

// +kubebuilder:object:root=true

// A EntityWorkflow is an example API type.
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="SYNCED",type="string",JSONPath=".status.conditions[?(@.type=='Synced')].status"
// +kubebuilder:printcolumn:name="EXTERNAL-NAME",type="string",JSONPath=".metadata.annotations.crossplane\\.io/external-name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories={crossplane,managed,tempoplane}
type EntityWorkflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EntityWorkflowSpec   `json:"spec"`
	Status EntityWorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EntityWorkflowList contains a list of EntityWorkflow
type EntityWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EntityWorkflow `json:"items"`
}

// EntityWorkflow type metadata.
var (
	EntityWorkflowKind             = reflect.TypeOf(EntityWorkflow{}).Name()
	EntityWorkflowGroupKind        = schema.GroupKind{Group: Group, Kind: EntityWorkflowKind}.String()
	EntityWorkflowKindAPIVersion   = EntityWorkflowKind + "." + SchemeGroupVersion.String()
	EntityWorkflowGroupVersionKind = SchemeGroupVersion.WithKind(EntityWorkflowKind)
)

func init() {
	SchemeBuilder.Register(&EntityWorkflow{}, &EntityWorkflowList{})
}
