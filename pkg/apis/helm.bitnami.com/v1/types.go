package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HelmRelease describes a Helm chart release.
type HelmRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmReleaseSpec   `json:"spec"`
	Status HelmReleaseStatus `json:"status"`
}

// HelmReleaseSpec is the spec for a HelmRelease resource.
type HelmReleaseSpec struct {
	// RepoURL is the URL of the repository. Defaults to stable repo.
	RepoURL string `json:"repoURL,omitempty"`
	// ChartName is the name of the chart within the repo
	ChartName string `json:"chartName,omitempty"`
	// Version is the chart version
	Version string `json:"version,omitempty"`
	// Username/Password required if repository is private
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// RawValues is a raw string containing extra Values added to the chart.
	// These values override the default values inside of the chart.
	RawValues string `json:"values,omitempty"`
	// Force if set, force resource update through delete/recreate if needed
	Force bool `json:"force,omitempty"`
	// Recreate if set, performs pod restart during upgrade/rollback
	Recreate bool `json:"recreate,omitempty"`
	// Paused is when a HelmRelease is paused, no actions except for deletion
	// will be performed on the underlying objects.
	Paused bool `json:"paused,omitempty"`
	// Description is human-friendly "log entry" about this helmrelease.
	Description string `json:"description,omitempty"`
}

// HelmRealeasePhase represents the current life-cycle phase of a HelmRelease.
type HelmRealeasePhase string

const (
	// HelmRealeasePhaseUnknown means that the helmrelease hasn't yet been processed.
	HelmRealeasePhaseUnknown HelmRealeasePhase = ""
	// HelmRealeasePhaseReady means the helmrelease has install/upgrade successfully.
	HelmRealeasePhaseReady HelmRealeasePhase = "Ready"
	// HelmRealeasePhaseFailed means the helmrelease has terminated with an error.
	HelmRealeasePhaseFailed HelmRealeasePhase = "Failed"
)

// HelmReleaseStatus captures the current status of a HelmRelease.
type HelmReleaseStatus struct {
	// ChartURl is final download link of chart
	ChartURL string `json:"chartUrl,omitempty"`
	// Phase is current helmrelease life-cycle phase
	Phase HelmRealeasePhase `json:"phase"`
	// Revision is current helm chart release revision
	Revision int32 `json:"revision,omitempty"`
	// FailMsg is error message
	FailMsg string `json:"failMsg,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HelmReleaseList is a list of HelmRelease resources
type HelmReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []HelmRelease `json:"items"`
}
