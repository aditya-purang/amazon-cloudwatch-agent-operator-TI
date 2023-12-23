// Copyright The OpenTelemetry Authors
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

// Package controllers contains the main controller, where the reconciliation starts.
package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyV1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-cloudwatch-agent-operator/apis/v1alpha1"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/config"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/manifests"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/status"
	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/autodetect"
	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/collector/reconcile"
	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/featuregate"
)

// AmazonCloudWatchAgentReconciler reconciles a AmazonCloudWatchAgent object.
type AmazonCloudWatchAgentReconciler struct {
	client.Client
	recorder record.EventRecorder
	scheme   *runtime.Scheme
	log      logr.Logger
	config   config.Config

	tasks   []Task
	muTasks sync.RWMutex
}

// Task represents a reconciliation task to be executed by the reconciler.
type Task struct {
	Do          func(context.Context, manifests.Params) error
	Name        string
	BailOnError bool
}

// Params is the set of options to build a new openTelemetryCollectorReconciler.
type Params struct {
	client.Client
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Tasks    []Task
	Config   config.Config
}

func (r *AmazonCloudWatchAgentReconciler) onOpenShiftRoutesChange() error {
	plt := r.config.OpenShiftRoutes()
	var (
		routesIdx = -1
	)
	r.muTasks.Lock()
	for i, t := range r.tasks {
		// search for route reconciler
		switch t.Name {
		case "routes":
			routesIdx = i
		}
	}
	r.muTasks.Unlock()

	if err := r.addRouteTask(plt, routesIdx); err != nil {
		return err
	}

	return r.removeRouteTask(plt, routesIdx)
}

func (r *AmazonCloudWatchAgentReconciler) addRouteTask(ora autodetect.OpenShiftRoutesAvailability, routesIdx int) error {
	r.muTasks.Lock()
	defer r.muTasks.Unlock()
	// if exists and openshift routes are available
	if routesIdx == -1 && ora == autodetect.OpenShiftRoutesAvailable {
		r.tasks = append([]Task{{reconcile.Routes, "routes", true}}, r.tasks...)
	}
	return nil
}

func (r *AmazonCloudWatchAgentReconciler) removeRouteTask(ora autodetect.OpenShiftRoutesAvailability, routesIdx int) error {
	r.muTasks.Lock()
	defer r.muTasks.Unlock()
	if len(r.tasks) < routesIdx {
		return fmt.Errorf("can not remove route task from reconciler")
	}
	// if exists and openshift routes are not available
	if routesIdx != -1 && ora == autodetect.OpenShiftRoutesNotAvailable {
		r.tasks = append(r.tasks[:routesIdx], r.tasks[routesIdx+1:]...)
	}
	return nil
}

func (r *AmazonCloudWatchAgentReconciler) getParams(instance v1alpha1.AmazonCloudWatchAgent) manifests.Params {
	return manifests.Params{
		Config:   r.config,
		Client:   r.Client,
		OtelCol:  instance,
		Log:      r.log,
		Scheme:   r.scheme,
		Recorder: r.recorder,
	}
}

// NewReconciler creates a new reconciler for AmazonCloudWatchAgent objects.
func NewReconciler(p Params) *AmazonCloudWatchAgentReconciler {
	r := &AmazonCloudWatchAgentReconciler{
		Client:   p.Client,
		log:      p.Log,
		scheme:   p.Scheme,
		config:   p.Config,
		tasks:    p.Tasks,
		recorder: p.Recorder,
	}

	if len(r.tasks) == 0 {
		// TODO: put this in line with the rest of how we generate manifests
		// https://github.com/aws/amazon-cloudwatch-agent-operator/issues/2108
		r.config.RegisterOpenShiftRoutesChangeCallback(r.onOpenShiftRoutesChange)
	}
	return r
}

// +kubebuilder:rbac:groups="",resources=configmaps;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets;deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes;routes/custom-host,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opentelemetry.io,resources=AmazonCloudWatchAgents,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=opentelemetry.io,resources=AmazonCloudWatchAgents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opentelemetry.io,resources=AmazonCloudWatchAgents/finalizers,verbs=get;update;patch

// Reconcile the current state of an OpenTelemetry collector resource with the desired state.
func (r *AmazonCloudWatchAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("amazoncloudwatchagent", req.NamespacedName)

	var instance v1alpha1.AmazonCloudWatchAgent
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to fetch AmazonCloudWatchAgent")
		}

		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	params := r.getParams(instance)
	if err := r.RunTasks(ctx, params); err != nil {
		return ctrl.Result{}, err
	}

	desiredObjects, buildErr := BuildCollector(params)
	if buildErr != nil {
		return ctrl.Result{}, buildErr
	}
	err := reconcileDesiredObjects(ctx, r.Client, log, &params.OtelCol, params.Scheme, desiredObjects...)
	return status.HandleReconcileStatus(ctx, log, params, err)
}

// RunTasks runs all the tasks associated with this reconciler.
func (r *AmazonCloudWatchAgentReconciler) RunTasks(ctx context.Context, params manifests.Params) error {
	r.muTasks.RLock()
	defer r.muTasks.RUnlock()
	for _, task := range r.tasks {
		if err := task.Do(ctx, params); err != nil {
			// If we get an error that occurs because a pod is being terminated, then exit this loop
			if apierrors.IsForbidden(err) && apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				r.log.V(2).Info("Exiting reconcile loop because namespace is being terminated", "namespace", params.OtelCol.Namespace)
				return nil
			}
			r.log.Error(err, fmt.Sprintf("failed to reconcile %s", task.Name))
			if task.BailOnError {
				return err
			}
		}
	}
	return nil
}

// SetupWithManager tells the manager what our controller is interested in.
func (r *AmazonCloudWatchAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := r.config.AutoDetect() // We need to call this, so we can get the correct autodetect version
	if err != nil {
		return err
	}
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.AmazonCloudWatchAgent{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.StatefulSet{})

	if featuregate.PrometheusOperatorIsAvailable.IsEnabled() {
		builder.Owns(&monitoringv1.ServiceMonitor{})
	}

	builder = builder.Owns(&autoscalingv2.HorizontalPodAutoscaler{})
	builder = builder.Owns(&policyV1.PodDisruptionBudget{})

	return builder.Complete(r)
}
