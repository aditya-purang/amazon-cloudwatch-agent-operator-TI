// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package auto

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/exp/maps"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/instrumentation"
)

const (
	autoAnnotatePrefix     = "cloudwatch.aws.amazon.com/auto-annotate-"
	defaultAnnotationValue = "true"
)

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=list;watch;update

// AnnotationMutators has an AnnotationMutator resource name
type AnnotationMutators struct {
	client              client.Client
	clientReader        client.Reader
	logger              logr.Logger
	namespaceMutators   map[string]instrumentation.AnnotationMutator
	deploymentMutators  map[string]instrumentation.AnnotationMutator
	daemonSetMutators   map[string]instrumentation.AnnotationMutator
	statefulSetMutators map[string]instrumentation.AnnotationMutator
	defaultMutator      instrumentation.AnnotationMutator
}

// MutateAll runs the mutators for each of the configured resources.
func (m *AnnotationMutators) MutateAll(ctx context.Context) {
	m.MutateNamespaces(ctx)
	m.MutateDeployments(ctx)
	m.MutateDaemonSets(ctx)
	m.MutateStatefulSets(ctx)
}

// MutateNamespaces lists all namespaces and runs MutateNamespace on each.
func (m *AnnotationMutators) MutateNamespaces(ctx context.Context) {
	namespaces := &corev1.NamespaceList{}
	if err := m.clientReader.List(ctx, namespaces); err != nil {
		m.logger.Error(err, "Unable to list namespaces")
		return
	}

	for _, namespace := range namespaces.Items {
		if m.Mutate(&namespace) {
			if err := m.client.Update(ctx, &namespace); err != nil {
				m.logger.Error(err, "Unable to send update",
					"kind", namespace.Kind,
					"name", namespace.Name,
				)
			}
		}
	}
}

// MutateDeployments lists all deployments and runs MutateDeployment on each.
func (m *AnnotationMutators) MutateDeployments(ctx context.Context) {
	deployments := &appsv1.DeploymentList{}
	if err := m.clientReader.List(ctx, deployments); err != nil {
		m.logger.Error(err, "Unable to list deployments")
		return
	}
	for _, deployment := range deployments.Items {
		if m.Mutate(&deployment) {
			if err := m.client.Update(ctx, &deployment); err != nil {
				m.logger.Error(err, "Unable to send update",
					"kind", deployment.Kind,
					"name", deployment.Name,
					"namespace", deployment.Namespace,
				)
			}
		}
	}
}

// MutateDaemonSets lists all daemonsets and runs MutateDaemonSet on each.
func (m *AnnotationMutators) MutateDaemonSets(ctx context.Context) {
	daemonSets := &appsv1.DaemonSetList{}
	if err := m.clientReader.List(ctx, daemonSets); err != nil {
		m.logger.Error(err, "Unable to list daemonsets")
		return
	}
	for _, daemonSet := range daemonSets.Items {
		if m.Mutate(&daemonSet) {
			if err := m.client.Update(ctx, &daemonSet); err != nil {
				m.logger.Error(err, "Unable to send update",
					"kind", daemonSet.Kind,
					"name", daemonSet.Name,
					"namespace", daemonSet.Namespace,
				)
			}
		}
	}
}

// MutateStatefulSets lists all statefulsets and runs MutateStatefulSet on each.
func (m *AnnotationMutators) MutateStatefulSets(ctx context.Context) {
	statefulSets := &appsv1.StatefulSetList{}
	if err := m.clientReader.List(ctx, statefulSets); err != nil {
		m.logger.Error(err, "Unable to list statefulsets")
		return
	}
	for _, statefulSet := range statefulSets.Items {
		if m.Mutate(&statefulSet) {
			if err := m.client.Update(ctx, &statefulSet); err != nil {
				m.logger.Error(err, "Unable to send update",
					"kind", statefulSet.Kind,
					"name", statefulSet.Name,
					"namespace", statefulSet.Namespace,
				)
			}
		}
	}
}

// Mutate modifies annotations for a single object using the configured mutators.
func (m *AnnotationMutators) Mutate(obj runtime.Object) bool {
	switch o := obj.(type) {
	case *corev1.Namespace:
		return m.mutate(o.GetName(), m.namespaceMutators, o.GetObjectMeta())
	case *appsv1.Deployment:
		return m.mutate(namespacedName(o.GetObjectMeta()), m.deploymentMutators, o.Spec.Template.GetObjectMeta())
	case *appsv1.DaemonSet:
		return m.mutate(namespacedName(o.GetObjectMeta()), m.daemonSetMutators, o.Spec.Template.GetObjectMeta())
	case *appsv1.StatefulSet:
		return m.mutate(namespacedName(o.GetObjectMeta()), m.statefulSetMutators, o.Spec.Template.GetObjectMeta())
	default:
		return false
	}
}

func (m *AnnotationMutators) mutate(name string, mutators map[string]instrumentation.AnnotationMutator, obj metav1.Object) bool {
	mutator, ok := mutators[name]
	if !ok {
		mutator = m.defaultMutator
	}
	return mutator.Mutate(obj)
}

func namespacedName(obj metav1.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())
}

// NewAnnotationMutators creates mutators based on the AnnotationConfig provided and enabled instrumentation.TypeSet.
// The default mutator, which is used for non-configured resources, removes all auto-annotated annotations in the type
// set.
func NewAnnotationMutators(client client.Client, clientReader client.Reader, logger logr.Logger, cfg AnnotationConfig, typeSet instrumentation.TypeSet) *AnnotationMutators {
	builder := newMutatorBuilder(typeSet)
	return &AnnotationMutators{
		client:              client,
		clientReader:        clientReader,
		logger:              logger,
		namespaceMutators:   builder.buildMutators(getResources(cfg, typeSet, getNamespaces)),
		deploymentMutators:  builder.buildMutators(getResources(cfg, typeSet, getDeployments)),
		daemonSetMutators:   builder.buildMutators(getResources(cfg, typeSet, getDaemonSets)),
		statefulSetMutators: builder.buildMutators(getResources(cfg, typeSet, getStatefulSets)),
		defaultMutator:      instrumentation.NewAnnotationMutator(maps.Values(builder.removeMutations)),
	}
}

func getResources(cfg AnnotationConfig, typeSet instrumentation.TypeSet, resourceFn func(AnnotationResources) []string) map[instrumentation.Type][]string {
	resources := map[instrumentation.Type][]string{}
	for instType := range typeSet {
		resources[instType] = resourceFn(cfg.getResources(instType))
	}
	return resources
}

type mutatorBuilder struct {
	typeSet         instrumentation.TypeSet
	insertMutations map[instrumentation.Type]instrumentation.AnnotationMutation
	removeMutations map[instrumentation.Type]instrumentation.AnnotationMutation
}

func (b *mutatorBuilder) buildMutators(resources map[instrumentation.Type][]string) map[string]instrumentation.AnnotationMutator {
	mutators := map[string]instrumentation.AnnotationMutator{}
	typeSetByResource := map[string]instrumentation.TypeSet{}
	for instType, resourceNames := range resources {
		for _, resourceName := range resourceNames {
			typeSet, ok := typeSetByResource[resourceName]
			if !ok {
				typeSet = instrumentation.NewTypeSet()
			}
			typeSet[instType] = nil
			typeSetByResource[resourceName] = typeSet
		}
	}
	for resourceName, typeSet := range typeSetByResource {
		var mutations []instrumentation.AnnotationMutation
		for instType := range b.typeSet {
			if _, ok := typeSet[instType]; ok {
				mutations = append(mutations, b.insertMutations[instType])
			} else {
				mutations = append(mutations, b.removeMutations[instType])
			}
		}
		mutators[resourceName] = instrumentation.NewAnnotationMutator(mutations)
	}
	return mutators
}

func newMutatorBuilder(typeSet instrumentation.TypeSet) *mutatorBuilder {
	builder := &mutatorBuilder{
		typeSet:         typeSet,
		insertMutations: map[instrumentation.Type]instrumentation.AnnotationMutation{},
		removeMutations: map[instrumentation.Type]instrumentation.AnnotationMutation{},
	}
	for instType := range typeSet {
		builder.insertMutations[instType], builder.removeMutations[instType] = buildMutations(instType)
	}
	return builder
}

// buildMutations builds insert and remove annotation mutations for the instrumentation.Type.
// Both are configured to only modify the annotations if all annotation keys are missing or present respectively.
func buildMutations(instType instrumentation.Type) (instrumentation.AnnotationMutation, instrumentation.AnnotationMutation) {
	annotations := buildAnnotations(instType)
	return instrumentation.NewInsertAnnotationMutation(annotations),
		instrumentation.NewRemoveAnnotationMutation(maps.Keys(annotations))
}

// buildAnnotations creates an annotation map of the inject and auto-annotate keys.
func buildAnnotations(instType instrumentation.Type) map[string]string {
	return map[string]string{
		instrumentation.InjectAnnotationKey(instType): defaultAnnotationValue,
		AnnotateKey(instType):                         defaultAnnotationValue,
	}
}

// AnnotateKey joins the auto-annotate annotation prefix with the provided instrumentation.Type.
func AnnotateKey(instType instrumentation.Type) string {
	return autoAnnotatePrefix + strings.ToLower(string(instType))
}