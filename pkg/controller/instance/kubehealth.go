// Copyright 2026 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package instance

import (
	"fmt"

	"github.com/kubehealth-project/kubehealth"
	healthv1alpha1 "github.com/kubehealth-project/kubehealth/api/v1alpha1"
	"github.com/kubehealth-project/kubehealth/catalog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubernetes-sigs/kro/pkg/graphengine/compiler"
	geruntime "github.com/kubernetes-sigs/kro/pkg/graphengine/runtime"
)

var standardKubeHealthAssessor, standardKubeHealthAssessorErr = catalog.NewAssessor()

const (
	kubeHealthChildrenReason        = "ChildResourcesAggregated"
	kubeHealthNoChildrenReason      = "NoApplicableChildResources"
	kubeHealthChildrenPendingReason = "ChildResourcesPending"
	kubeHealthChildrenFailedReason  = "ChildResourceReconciliationFailed"
	kubeHealthTerminatingReason     = "DeletionRequested"
	kubeHealthActiveReason          = "DeletionNotRequested"
	kubeHealthAssessmentErrorReason = "ChildAssessmentFailed"
)

// propagatedKubeHealth assesses every concrete resource observed by the graph
// runtime and builds the operator-reported status for the generated instance.
// Def, ref, patch, and graph nodes are orchestration details or resources
// outside the instance's ownership, so they do not participate.
func propagatedKubeHealth(
	inst *unstructured.Unstructured,
	rt *geruntime.Runtime,
	previous map[string]any,
	reconciling bool,
	degraded bool,
) (map[string]any, error) {
	health := previousKubeHealth(previous)
	if health == nil {
		health = healthv1alpha1.NewStatus(inst.GetGeneration())
	}
	health.ContractVersion = healthv1alpha1.ContractVersion
	health.ObservedGeneration = inst.GetGeneration()

	assessments, observed, pending, err := childAssessments(rt)
	if err != nil {
		health.SetReconciliation(
			kubehealth.ReconciliationUnknown,
			kubeHealthAssessmentErrorReason,
			fmt.Sprintf("Unable to assess child resources: %v", err),
		)
		health.SetAvailability(
			kubehealth.AvailabilityUnknown,
			kubeHealthAssessmentErrorReason,
			"Child resource availability could not be determined",
		)
	} else {
		aggregated := kubehealth.AggregateChildren(assessments)
		reason := kubeHealthChildrenReason
		message := fmt.Sprintf("Aggregated health from %d child resource(s)", observed)
		if pending > 0 {
			reason = kubeHealthChildrenPendingReason
			message = fmt.Sprintf(
				"Aggregated health from %d observed child resource(s); %d resource node(s) are pending",
				observed,
				pending,
			)
		}
		if len(assessments) == 0 {
			reason = kubeHealthNoChildrenReason
			message = "The graph has no observed child resources to assess"
		}
		if degraded {
			aggregated.Reconciliation = kubehealth.ReconciliationFailed
			reason = kubeHealthChildrenFailedReason
			message = "One or more child resources failed to reconcile"
		} else if reconciling && aggregated.Reconciliation != kubehealth.ReconciliationFailed {
			aggregated.Reconciliation = kubehealth.ReconciliationInProgress
			reason = kubeHealthChildrenPendingReason
			message = "The child resource graph is still reconciling"
		}
		health.SetReconciliation(aggregated.Reconciliation, reason, message)
		health.SetAvailability(aggregated.Availability, reason, message)
	}

	if inst.GetDeletionTimestamp() != nil {
		health.SetLifecycle(
			kubehealth.LifecycleTerminating,
			kubeHealthTerminatingReason,
			"The instance is terminating",
		)
	} else {
		health.SetLifecycle(
			kubehealth.LifecycleActive,
			kubeHealthActiveReason,
			"The instance is active",
		)
	}

	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(health)
	if err != nil {
		return nil, fmt.Errorf("encode propagated KubeHealth status: %w", err)
	}
	return value, nil
}

func childAssessments(rt *geruntime.Runtime) ([]kubehealth.Assessment, int, int, error) {
	if standardKubeHealthAssessorErr != nil {
		return nil, 0, 0, fmt.Errorf("initialize KubeHealth catalog: %w", standardKubeHealthAssessorErr)
	}
	if rt == nil {
		return nil, 0, 0, nil
	}
	var assessments []kubehealth.Assessment
	observed := 0
	pending := 0
	for _, node := range rt.Nodes() {
		if node.Kind() != compiler.NodeKindTemplate {
			continue
		}
		ignored, err := node.IsIgnored()
		if err == nil && ignored {
			continue
		}
		children := node.Observed()
		if children == nil {
			pending++
			assessments = append(assessments, kubehealth.Assessment{
				Reconciliation: kubehealth.Dimension[kubehealth.ReconciliationStatus]{
					Status:  kubehealth.ReconciliationInProgress,
					Reason:  kubeHealthChildrenPendingReason,
					Message: fmt.Sprintf("Resource node %q has not been observed", node.ID()),
				},
				Availability: kubehealth.Dimension[kubehealth.AvailabilityStatus]{
					Status: kubehealth.AvailabilityUnknown,
				},
				Lifecycle: kubehealth.Dimension[kubehealth.LifecycleStatus]{
					Status: kubehealth.LifecycleActive,
				},
			})
			continue
		}
		for _, child := range children {
			assessment, err := standardKubeHealthAssessor.Assess(child)
			if err != nil {
				return nil, observed, pending, fmt.Errorf("assess resource node %q: %w", node.ID(), err)
			}
			assessments = append(assessments, assessment)
			observed++
		}
	}
	return assessments, observed, pending, nil
}

func propagatedNodeFreeKubeHealth(
	inst *unstructured.Unstructured,
	previous map[string]any,
	reconciliation kubehealth.ReconciliationStatus,
	reason string,
	message string,
) (map[string]any, error) {
	health := previousKubeHealth(previous)
	if health == nil {
		health = healthv1alpha1.NewStatus(inst.GetGeneration())
		health.Reconciliation.Status = kubehealth.ReconciliationUnknown
		health.Availability.Status = kubehealth.AvailabilityUnknown
	}
	health.ContractVersion = healthv1alpha1.ContractVersion
	health.ObservedGeneration = inst.GetGeneration()
	if reconciliation != "" {
		health.SetReconciliation(reconciliation, reason, message)
	}

	if inst.GetDeletionTimestamp() != nil {
		health.SetLifecycle(
			kubehealth.LifecycleTerminating,
			kubeHealthTerminatingReason,
			"The instance is terminating",
		)
	} else {
		health.SetLifecycle(
			kubehealth.LifecycleActive,
			kubeHealthActiveReason,
			"The instance is active",
		)
	}

	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(health)
	if err != nil {
		return nil, fmt.Errorf("encode propagated KubeHealth status: %w", err)
	}
	return value, nil
}

func previousKubeHealth(status map[string]any) *healthv1alpha1.Status {
	value, ok := status["kubeHealth"].(map[string]any)
	if !ok {
		return nil
	}
	var health healthv1alpha1.Status
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(value, &health); err != nil {
		return nil
	}
	if err := health.Validate(); err != nil {
		return nil
	}
	return &health
}
