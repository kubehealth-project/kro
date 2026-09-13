// Copyright 2025 The Kubernetes Authors.
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

package crd

import (
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var (
	defaultStateType = extv1.JSONSchemaProps{
		Type: "string",
	}
	defaultConditionsType = extv1.JSONSchemaProps{
		Type: "array",
		Items: &extv1.JSONSchemaPropsOrArray{
			Schema: &extv1.JSONSchemaProps{
				Type: "object",
				Properties: map[string]extv1.JSONSchemaProps{
					"type": {
						Type: "string", // Boolean maybe?
					},
					"status": {
						Type: "string",
					},
					"reason": {
						Type: "string",
					},
					"message": {
						Type: "string",
					},
					"lastTransitionTime": {
						Type: "string",
					},
					"observedGeneration": {
						Type: "integer",
					},
				},
			},
		},
	}
	kubeHealthReconciliationDimensionType = kubeHealthDimensionSchema(
		"Reconciled", "InProgress", "Failed", "Suspended", "Unknown", "NotApplicable",
	)
	kubeHealthAvailabilityDimensionType = kubeHealthDimensionSchema(
		"Available", "PartiallyAvailable", "Unavailable", "Unknown", "NotApplicable",
	)
	kubeHealthLifecycleDimensionType = kubeHealthDimensionSchema(
		"Active", "Terminating",
	)
	defaultKubeHealthType = extv1.JSONSchemaProps{
		Type:     "object",
		Required: []string{"contractVersion", "observedGeneration", "reconciliation", "availability", "lifecycle"},
		Properties: map[string]extv1.JSONSchemaProps{
			"contractVersion": {
				Type: "string",
				Enum: []extv1.JSON{{Raw: []byte(`"v1alpha1"`)}},
			},
			"observedGeneration": {
				Type:    "integer",
				Minimum: new(float64),
			},
			"reconciliation": kubeHealthReconciliationDimensionType,
			"availability":   kubeHealthAvailabilityDimensionType,
			"lifecycle":      kubeHealthLifecycleDimensionType,
		},
	}
	// additionalPrinterColumns specifies additional columns returned in Table output.
	// See https://kubernetes.io/docs/reference/using-api/api-concepts/#receiving-resources-as-tables for details.
	// Sample output for `kubectl get clusters`
	//
	// NAME            STATE    READY    AGE
	// testcluster29   ACTIVE   True     22d
	defaultAdditionalPrinterColumns = []extv1.CustomResourceColumnDefinition{
		// ResourceGraphDefinition instance state
		{
			Name:        "State",
			Description: "The state of a ResourceGraphDefinition instance",
			Priority:    0,
			Type:        "string",
			JSONPath:    ".status.state",
		},
		// ResourceGraphDefinition instance Ready condition
		{
			Name:        "Ready",
			Description: "Whether a ResourceGraphDefinition instance is ready",
			Priority:    0,
			Type:        "string",
			JSONPath:    ".status.conditions[?(@.type==\"Ready\")].status",
		},
		// ResourceGraphDefinition instance age
		{
			Name:        "Age",
			Description: "Age of the resource",
			Priority:    0,
			Type:        "date",
			JSONPath:    ".metadata.creationTimestamp",
		},
	}
)

func kubeHealthDimensionSchema(statuses ...string) extv1.JSONSchemaProps {
	enum := make([]extv1.JSON, len(statuses))
	for i, status := range statuses {
		enum[i] = extv1.JSON{Raw: []byte(`"` + status + `"`)}
	}
	return extv1.JSONSchemaProps{
		Type:     "object",
		Required: []string{"status"},
		Properties: map[string]extv1.JSONSchemaProps{
			"status": {
				Type: "string",
				Enum: enum,
			},
			"reason": {
				Type:      "string",
				MaxLength: new(int64(1024)),
				Pattern:   `^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$`,
			},
			"message": {
				Type:      "string",
				MaxLength: new(int64(32768)),
			},
			"lastTransitionTime": {
				Type:   "string",
				Format: "date-time",
			},
		},
	}
}
