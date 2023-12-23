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

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/aws/amazon-cloudwatch-agent-operator/internal/config"
)

var (
	_ admission.CustomValidator = &CollectorWebhook{}
	_ admission.CustomDefaulter = &CollectorWebhook{}
)

// +kubebuilder:webhook:path=/mutate-cloudwatch-aws-amazon-com-v1alpha1-amazoncloudwatchagent,mutating=true,failurePolicy=fail,groups=opentelemetry.io,resources=amazoncloudwatchagents,verbs=create;update,versions=v1alpha1,name=mamazoncloudwatchagent.kb.io,sideEffects=none,admissionReviewVersions=v1
// +kubebuilder:webhook:verbs=create;update,path=/validate-cloudwatch-aws-amazon-com-v1alpha1-amazoncloudwatchagent,mutating=false,failurePolicy=fail,groups=opentelemetry.io,resources=amazoncloudwatchagents,versions=v1alpha1,name=vamazoncloudwatchagentcreateupdate.kb.io,sideEffects=none,admissionReviewVersions=v1
// +kubebuilder:webhook:verbs=delete,path=/validate-cloudwatch-aws-amazon-com-v1alpha1-amazoncloudwatchagent,mutating=false,failurePolicy=ignore,groups=opentelemetry.io,resources=amazoncloudwatchagents,versions=v1alpha1,name=vamazoncloudwatchagentdelete.kb.io,sideEffects=none,admissionReviewVersions=v1
// +kubebuilder:object:generate=false

type CollectorWebhook struct {
	logger logr.Logger
	cfg    config.Config
	scheme *runtime.Scheme
}

func (c CollectorWebhook) Default(ctx context.Context, obj runtime.Object) error {
	otelcol, ok := obj.(*AmazonCloudWatchAgent)
	if !ok {
		return fmt.Errorf("expected an AmazonCloudWatchAgent, received %T", obj)
	}
	return c.defaulter(otelcol)
}

func (c CollectorWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	otelcol, ok := obj.(*AmazonCloudWatchAgent)
	if !ok {
		return nil, fmt.Errorf("expected an AmazonCloudWatchAgent, received %T", obj)
	}
	return c.validate(otelcol)
}

func (c CollectorWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	otelcol, ok := newObj.(*AmazonCloudWatchAgent)
	if !ok {
		return nil, fmt.Errorf("expected an AmazonCloudWatchAgent, received %T", newObj)
	}
	return c.validate(otelcol)
}

func (c CollectorWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	otelcol, ok := obj.(*AmazonCloudWatchAgent)
	if !ok || otelcol == nil {
		return nil, fmt.Errorf("expected an AmazonCloudWatchAgent, received %T", obj)
	}
	return c.validate(otelcol)
}

func (c CollectorWebhook) defaulter(r *AmazonCloudWatchAgent) error {
	if len(r.Spec.Mode) == 0 {
		r.Spec.Mode = ModeDeployment
	}
	if len(r.Spec.UpgradeStrategy) == 0 {
		r.Spec.UpgradeStrategy = UpgradeStrategyAutomatic
	}

	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if r.Labels["app.kubernetes.io/managed-by"] == "" {
		r.Labels["app.kubernetes.io/managed-by"] = "amazon-cloudwatch-agent-operator"
	}

	// We can default to one because dependent objects Deployment and HorizontalPodAutoScaler
	// default to 1 as well.
	one := int32(1)
	if r.Spec.Replicas == nil {
		r.Spec.Replicas = &one
	}

	if r.Spec.Ingress.Type == IngressTypeRoute && r.Spec.Ingress.Route.Termination == "" {
		r.Spec.Ingress.Route.Termination = TLSRouteTerminationTypeEdge
	}

	return nil
}

func (c CollectorWebhook) validate(r *AmazonCloudWatchAgent) (admission.Warnings, error) {
	warnings := admission.Warnings{}
	// validate volumeClaimTemplates
	if r.Spec.Mode != ModeStatefulSet && len(r.Spec.VolumeClaimTemplates) > 0 {
		return warnings, fmt.Errorf("the OpenTelemetry Collector mode is set to %s, which does not support the attribute 'volumeClaimTemplates'", r.Spec.Mode)
	}

	// validate tolerations
	if r.Spec.Mode == ModeSidecar && len(r.Spec.Tolerations) > 0 {
		return warnings, fmt.Errorf("the OpenTelemetry Collector mode is set to %s, which does not support the attribute 'tolerations'", r.Spec.Mode)
	}

	// validate priorityClassName
	if r.Spec.Mode == ModeSidecar && r.Spec.PriorityClassName != "" {
		return warnings, fmt.Errorf("the OpenTelemetry Collector mode is set to %s, which does not support the attribute 'priorityClassName'", r.Spec.Mode)
	}

	// validator port config
	for _, p := range r.Spec.Ports {
		nameErrs := validation.IsValidPortName(p.Name)
		numErrs := validation.IsValidPortNum(int(p.Port))
		if len(nameErrs) > 0 || len(numErrs) > 0 {
			return warnings, fmt.Errorf("the OpenTelemetry Spec Ports configuration is incorrect, port name '%s' errors: %s, num '%d' errors: %s",
				p.Name, nameErrs, p.Port, numErrs)
		}
	}

	if r.Spec.Ingress.Type == IngressTypeNginx && r.Spec.Mode == ModeSidecar {
		return warnings, fmt.Errorf("the OpenTelemetry Spec Ingress configuration is incorrect. Ingress can only be used in combination with the modes: %s, %s, %s",
			ModeDeployment, ModeDaemonSet, ModeStatefulSet,
		)
	}

	if r.Spec.Ingress.Type == IngressTypeNginx && r.Spec.Mode == ModeSidecar {
		return warnings, fmt.Errorf("the OpenTelemetry Spec Ingress configuiration is incorrect. Ingress can only be used in combination with the modes: %s, %s, %s",
			ModeDeployment, ModeDaemonSet, ModeStatefulSet,
		)
	}

	return warnings, nil
}

func SetupCollectorWebhook(mgr ctrl.Manager, cfg config.Config) error {
	cvw := &CollectorWebhook{
		logger: mgr.GetLogger().WithValues("handler", "CollectorWebhook"),
		scheme: mgr.GetScheme(),
		cfg:    cfg,
	}
	return ctrl.NewWebhookManagedBy(mgr).
		For(&AmazonCloudWatchAgent{}).
		WithValidator(cvw).
		WithDefaulter(cvw).
		Complete()
}
