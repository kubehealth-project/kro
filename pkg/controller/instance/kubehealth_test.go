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
	"testing"

	"github.com/kubehealth-project/kubehealth"
	healthv1alpha1 "github.com/kubehealth-project/kubehealth/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-openapi/pkg/validation/spec"

	"github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/graphengine/compiler"
	geruntime "github.com/kubernetes-sigs/kro/pkg/graphengine/runtime"
)

func TestPropagatedKubeHealthAggregatesObservedChildren(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	inst.SetGeneration(7)
	rt := runtimeWithKubeHealthChildren(
		healthyKubeHealthDeployment("app"),
		newConfigMapObject("config", "default"),
	)

	got, err := propagatedKubeHealth(inst, rt, nil, false, false)
	require.NoError(t, err)
	assert.Equal(t, healthv1alpha1.ContractVersion, got["contractVersion"])
	assert.Equal(t, int64(7), got["observedGeneration"])
	assert.Equal(t, string(kubehealth.ReconciliationReconciled), nestedStatus(t, got, "reconciliation"))
	assert.Equal(t, string(kubehealth.AvailabilityAvailable), nestedStatus(t, got, "availability"))
	assert.Equal(t, string(kubehealth.LifecycleActive), nestedStatus(t, got, "lifecycle"))
}

func TestPropagatedKubeHealthMarksUnobservedTemplatePending(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	rt := runtimeWithKubeHealthChildren(nil)

	got, err := propagatedKubeHealth(inst, rt, nil, false, false)
	require.NoError(t, err)
	assert.Equal(t, string(kubehealth.ReconciliationInProgress), nestedStatus(t, got, "reconciliation"))
	assert.Equal(t, string(kubehealth.AvailabilityUnknown), nestedStatus(t, got, "availability"))
	assert.Equal(t, string(kubehealth.LifecycleInitializing), nestedStatus(t, got, "lifecycle"))
	assert.Equal(t, kubeHealthChildrenPendingReason, nestedReason(t, got, "reconciliation"))
}

func TestPropagatedKubeHealthReflectsGraphReconciliationFailure(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	rt := runtimeWithKubeHealthChildren(healthyKubeHealthDeployment("app"))

	got, err := propagatedKubeHealth(inst, rt, nil, true, true)
	require.NoError(t, err)
	assert.Equal(t, string(kubehealth.ReconciliationFailed), nestedStatus(t, got, "reconciliation"))
	assert.Equal(t, string(kubehealth.AvailabilityAvailable), nestedStatus(t, got, "availability"))
	assert.Equal(t, kubeHealthChildrenFailedReason, nestedReason(t, got, "reconciliation"))
}

func TestPropagatedKubeHealthKeepsFailedChildAvailable(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	deployment := healthyKubeHealthDeployment("app")
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:   appsv1.DeploymentProgressing,
		Status: corev1.ConditionFalse,
		Reason: "ProgressDeadlineExceeded",
	}}
	rt := runtimeWithKubeHealthChildren(deployment)

	got, err := propagatedKubeHealth(inst, rt, nil, false, false)
	require.NoError(t, err)
	assert.Equal(t, string(kubehealth.ReconciliationFailed), nestedStatus(t, got, "reconciliation"))
	assert.Equal(t, string(kubehealth.AvailabilityAvailable), nestedStatus(t, got, "availability"))
}

func TestPropagatedKubeHealthPreservesTransitionTime(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	inst.SetGeneration(2)
	previousTime := "2026-09-13T12:00:00Z"
	previous := map[string]any{
		"kubeHealth": map[string]any{
			"contractVersion":    healthv1alpha1.ContractVersion,
			"observedGeneration": int64(1),
			"reconciliation": map[string]any{
				"status":             string(kubehealth.ReconciliationInProgress),
				"lastTransitionTime": previousTime,
			},
			"availability": map[string]any{
				"status":             string(kubehealth.AvailabilityUnknown),
				"lastTransitionTime": previousTime,
			},
			"lifecycle": map[string]any{
				"status":             string(kubehealth.LifecycleInitializing),
				"lastTransitionTime": previousTime,
			},
		},
	}
	rt := runtimeWithKubeHealthChildren(nil)

	got, err := propagatedKubeHealth(inst, rt, previous, false, false)
	require.NoError(t, err)
	assert.Equal(t, previousTime, nestedTransitionTime(t, got, "reconciliation"))
	assert.Equal(t, previousTime, nestedTransitionTime(t, got, "availability"))
	assert.Equal(t, previousTime, nestedTransitionTime(t, got, "lifecycle"))
	assert.Equal(t, int64(2), got["observedGeneration"])
}

func TestPropagatedNodeFreeKubeHealthReportsTermination(t *testing.T) {
	inst := newInstanceObject("demo", "default")
	now := metav1.Now()
	inst.SetDeletionTimestamp(&now)

	got, err := propagatedNodeFreeKubeHealth(inst, nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, string(kubehealth.LifecycleTerminating), nestedStatus(t, got, "lifecycle"))
	assert.Equal(t, string(kubehealth.ReconciliationUnknown), nestedStatus(t, got, "reconciliation"))
}

func TestPreviousKubeHealthRejectsMalformedReport(t *testing.T) {
	assert.Nil(t, previousKubeHealth(map[string]any{
		"kubeHealth": map[string]any{"contractVersion": "unsupported"},
	}))
}

func runtimeWithKubeHealthChildren(children ...any) *geruntime.Runtime {
	nodes := make([]v1alpha1.Node, len(children))
	programNodes := make(map[string]*compiler.Node, len(children))
	order := make([]string, len(children))
	for i := range children {
		id := fmt.Sprintf("child%d", i)
		nodes[i] = v1alpha1.Node{ID: id}
		order[i] = id
		programNodes[id] = &compiler.Node{ID: id, Kind: compiler.NodeKindTemplate}
	}
	program := &compiler.Program{
		Nodes:            programNodes,
		TopologicalOrder: order,
		NodeSchemas:      map[string]*spec.Schema{},
	}
	rt := geruntime.New(program, &v1alpha1.Graph{Spec: v1alpha1.GraphSpec{Nodes: nodes}})
	for i, child := range children {
		if child == nil {
			continue
		}
		var obj *unstructured.Unstructured
		switch value := child.(type) {
		case *unstructured.Unstructured:
			obj = value
		default:
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(value)
			if err != nil {
				panic(err)
			}
			obj = &unstructured.Unstructured{Object: raw}
		}
		rt.Node(order[i]).SetObserved([]*unstructured.Unstructured{obj}, []*unstructured.Unstructured{obj})
	}
	return rt
}

func healthyKubeHealthDeployment(name string) *appsv1.Deployment {
	replicas := int32(2)
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Generation: 1},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           2,
			UpdatedReplicas:    2,
			ReadyReplicas:      2,
			AvailableReplicas:  2,
		},
	}
}

func nestedStatus(t *testing.T, health map[string]any, dimension string) string {
	t.Helper()
	value, found, err := unstructured.NestedString(health, dimension, "status")
	require.NoError(t, err)
	require.True(t, found)
	return value
}

func nestedReason(t *testing.T, health map[string]any, dimension string) string {
	t.Helper()
	value, found, err := unstructured.NestedString(health, dimension, "reason")
	require.NoError(t, err)
	require.True(t, found)
	return value
}

func nestedTransitionTime(t *testing.T, health map[string]any, dimension string) string {
	t.Helper()
	value, found, err := unstructured.NestedString(health, dimension, "lastTransitionTime")
	require.NoError(t, err)
	require.True(t, found)
	return value
}
