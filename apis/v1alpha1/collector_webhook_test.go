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
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/aws/amazon-cloudwatch-agent-operator/internal/config"
)

var (
	testScheme *runtime.Scheme = scheme.Scheme
)

func TestOTELColDefaultingWebhook(t *testing.T) {
	one := int32(1)
	five := int32(5)

	if err := AddToScheme(testScheme); err != nil {
		fmt.Printf("failed to register scheme: %v", err)
		os.Exit(1)
	}

	tests := []struct {
		name     string
		otelcol  AmazonCloudWatchAgent
		expected AmazonCloudWatchAgent
	}{
		{
			name:    "all fields default",
			otelcol: AmazonCloudWatchAgent{},
			expected: AmazonCloudWatchAgent{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
					},
				},
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeDeployment,
					Replicas:        &one,
					UpgradeStrategy: UpgradeStrategyAutomatic,
				},
			},
		},
		{
			name: "provided values in spec",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeSidecar,
					Replicas:        &five,
					UpgradeStrategy: "adhoc",
				},
			},
			expected: AmazonCloudWatchAgent{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
					},
				},
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeSidecar,
					Replicas:        &five,
					UpgradeStrategy: "adhoc",
				},
			},
		},
		{
			name: "doesn't override unmanaged",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeSidecar,
					Replicas:        &five,
					UpgradeStrategy: "adhoc",
				},
			},
			expected: AmazonCloudWatchAgent{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
					},
				},
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeSidecar,
					Replicas:        &five,
					UpgradeStrategy: "adhoc",
				},
			},
		},
		{
			name: "Missing route termination",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode: ModeDeployment,
					Ingress: Ingress{
						Type: IngressTypeRoute,
					},
				},
			},
			expected: AmazonCloudWatchAgent{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
					},
				},
				Spec: AmazonCloudWatchAgentSpec{
					Mode: ModeDeployment,
					Ingress: Ingress{
						Type: IngressTypeRoute,
						Route: OpenShiftRoute{
							Termination: TLSRouteTerminationTypeEdge,
						},
					},
					Replicas:        &one,
					UpgradeStrategy: UpgradeStrategyAutomatic,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cvw := &CollectorWebhook{
				logger: logr.Discard(),
				scheme: testScheme,
				cfg: config.New(
					config.WithCollectorImage("collector:v0.0.0"),
					config.WithTargetAllocatorImage("ta:v0.0.0"),
				),
			}
			ctx := context.Background()
			err := cvw.Default(ctx, &test.otelcol)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, test.otelcol)
		})
	}
}

// TODO: a lot of these tests use .Spec.MaxReplicas and .Spec.MinReplicas. These fields are
// deprecated and moved to .Spec.Autoscaler. Fine to use these fields to test that old CRD is
// still supported but should eventually be updated.
func TestOTELColValidatingWebhook(t *testing.T) {
	three := int32(3)

	tests := []struct { //nolint:govet
		name             string
		otelcol          AmazonCloudWatchAgent
		expectedErr      string
		expectedWarnings []string
	}{
		{
			name:    "valid empty spec",
			otelcol: AmazonCloudWatchAgent{},
		},
		{
			name: "valid full spec",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:            ModeStatefulSet,
					Replicas:        &three,
					UpgradeStrategy: "adhoc",
					Config: `receivers:
  examplereceiver:
    endpoint: "0.0.0.0:12345"
  examplereceiver/settings:
    endpoint: "0.0.0.0:12346"
  prometheus:
    config:
      scrape_configs:
        - job_name: otel-collector
          scrape_interval: 10s
  jaeger/custom:
    protocols:
      thrift_http:
        endpoint: 0.0.0.0:15268
`,
					Ports: []v1.ServicePort{
						{
							Name: "port1",
							Port: 5555,
						},
						{
							Name:     "port2",
							Port:     5554,
							Protocol: v1.ProtocolUDP,
						},
					},
				},
			},
		},
		{
			name: "invalid mode with volume claim templates",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:                 ModeSidecar,
					VolumeClaimTemplates: []v1.PersistentVolumeClaim{{}, {}},
				},
			},
			expectedErr: "does not support the attribute 'volumeClaimTemplates'",
		},
		{
			name: "invalid mode with tolerations",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:        ModeSidecar,
					Tolerations: []v1.Toleration{{}, {}},
				},
			},
			expectedErr: "does not support the attribute 'tolerations'",
		},
		{
			name: "invalid port name",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Ports: []v1.ServicePort{
						{
							// this port name contains a non alphanumeric character, which is invalid.
							Name:     "-test🦄port",
							Port:     12345,
							Protocol: v1.ProtocolTCP,
						},
					},
				},
			},
			expectedErr: "the OpenTelemetry Spec Ports configuration is incorrect",
		},
		{
			name: "invalid port name, too long",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Ports: []v1.ServicePort{
						{
							Name: "aaaabbbbccccdddd", // len: 16, too long
							Port: 5555,
						},
					},
				},
			},
			expectedErr: "the OpenTelemetry Spec Ports configuration is incorrect",
		},
		{
			name: "invalid port num",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Ports: []v1.ServicePort{
						{
							Name: "aaaabbbbccccddd", // len: 15
							// no port set means it's 0, which is invalid
						},
					},
				},
			},
			expectedErr: "the OpenTelemetry Spec Ports configuration is incorrect",
		},
		{
			name: "invalid deployment mode incompabible with ingress settings",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode: ModeSidecar,
					Ingress: Ingress{
						Type: IngressTypeNginx,
					},
				},
			},
			expectedErr: fmt.Sprintf("Ingress can only be used in combination with the modes: %s, %s, %s",
				ModeDeployment, ModeDaemonSet, ModeStatefulSet,
			),
		},
		{
			name: "invalid mode with priorityClassName",
			otelcol: AmazonCloudWatchAgent{
				Spec: AmazonCloudWatchAgentSpec{
					Mode:              ModeSidecar,
					PriorityClassName: "test-class",
				},
			},
			expectedErr: "does not support the attribute 'priorityClassName'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cvw := &CollectorWebhook{
				logger: logr.Discard(),
				scheme: testScheme,
				cfg: config.New(
					config.WithCollectorImage("collector:v0.0.0"),
					config.WithTargetAllocatorImage("ta:v0.0.0"),
				),
			}
			ctx := context.Background()
			warnings, err := cvw.ValidateCreate(ctx, &test.otelcol)
			if test.expectedErr == "" {
				assert.NoError(t, err)
				return
			}
			if len(test.expectedWarnings) == 0 {
				assert.Empty(t, warnings, test.expectedWarnings)
			} else {
				assert.ElementsMatch(t, warnings, test.expectedWarnings)
			}
			assert.ErrorContains(t, err, test.expectedErr)
		})
	}
}
