// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-cloudwatch-agent-operator/internal/manifests"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/naming"
)

// Deployment builds the deployment for the given instance.
func Deployment(params manifests.Params) *appsv1.Deployment {
	name := naming.Collector(params.OtelCol.Name)
	labels := Labels(params.OtelCol, name, params.Config.LabelsFilter())

	annotations := Annotations(params.OtelCol)
	podAnnotations := PodAnnotations(params.OtelCol)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   params.OtelCol.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: params.OtelCol.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(params.OtelCol),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountName(params.OtelCol),
					Containers:         []corev1.Container{Container(params.Config, params.Log, params.OtelCol, true)},
					Volumes:            Volumes(params.Config, params.OtelCol),
					DNSPolicy:          getDNSPolicy(params.OtelCol),
					HostNetwork:        params.OtelCol.Spec.HostNetwork,
					Tolerations:        params.OtelCol.Spec.Tolerations,
					NodeSelector:       params.OtelCol.Spec.NodeSelector,
					PriorityClassName:  params.OtelCol.Spec.PriorityClassName,
				},
			},
		},
	}
}
