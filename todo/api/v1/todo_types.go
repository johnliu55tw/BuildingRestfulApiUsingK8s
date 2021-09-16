/*
Copyright 2021.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TodoSpec defines the desired state of Todo
type TodoSpec struct {
	//TODO optional fields
	// Summary of the todo item
	Summary string `json:"summary"`

	// Whether this todo item is marked complete
	Complete bool `json:"complete"`

	// The email address to notify
	// +optional
	NotifyEmail string `json:"notifyEmail,omitempty"`

	// When to send out the notification
	// +optional
	NotifyAt *metav1.Time `json:"notifyAt,omitempty"`
}

// TodoStatus defines the observed state of Todo
type TodoStatus struct {

	// When is the todo being notified
	NotifiedAt *metav1.Time `json:"notifiedAt,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Todo is the Schema for the todoes API
type Todo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TodoSpec   `json:"spec,omitempty"`
	Status TodoStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TodoList contains a list of Todo
type TodoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Todo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Todo{}, &TodoList{})
}
