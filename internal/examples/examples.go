// Package examples embeds curated starter blueprints for Composition Factory.
//
// These blueprints showcase canonical Crossplane v2 composition patterns:
//   - IRSA: AWS IAM Role + native Kubernetes ServiceAccount with status ARN wire into annotations
//   - RDS: AWS RDS Database Instance with configurable parameters, credentials secret envelope, and multi-AZ
//   - K8s App: Full-stack composition combining native K8s Deployment/Service with AWS SQS and IRSA
package examples

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

//go:embed irsa.cf.yaml
var irsaYAML string

//go:embed rds.cf.yaml
var rdsYAML string

//go:embed k8s-app.cf.yaml
var k8sAppYAML string

//go:embed s3-bucket.cf.yaml
var s3BucketYAML string

//go:embed sqs-queue.cf.yaml
var sqsQueueYAML string

//go:embed k8s-workload.cf.yaml
var k8sWorkloadYAML string

//go:embed k8s-cronjob.cf.yaml
var k8sCronJobYAML string

// Example represents a curated starter blueprint available in Composition Factory.
type Example struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	ResourceCount int      `json:"resourceCount"`
	Sources       []string `json:"sources"`
	YAML          string   `json:"yaml"`
}

// All returns all available starter blueprints in canonical order.
func All() []Example {
	return []Example{
		{
			ID:          "irsa",
			Name:        "AWS IRSA (IAM Role + EKS ServiceAccount)",
			Description: "Production-ready IAM Role with scoped AssumeRole trust policy, S3 permissions policy, and native K8s ServiceAccount with role-arn status annotation wire.",
			Tags:        []string{"AWS", "IAM", "Kubernetes", "Status Wire", "Annotations"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0"},
			YAML:        strings.TrimSpace(irsaYAML),
		},
		{
			ID:          "rds-postgres",
			Name:        "AWS RDS PostgreSQL Database",
			Description: "Production-grade RDS PostgreSQL instance with storage autoscaling, backup retention, engine versioning, deletion protection, and credentials connection secret envelope.",
			Tags:        []string{"AWS", "RDS", "Database", "Envelopes", "Parameters"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0"},
			YAML:        strings.TrimSpace(rdsYAML),
		},
		{
			ID:          "k8s-app",
			Name:        "Full-Stack Microservice (App + SQS + IRSA + RDS)",
			Description: "Full-stack microservice composition combining native K8s Deployment & Service with an AWS SQS queue, IAM IRSA ServiceAccount, and AWS RDS PostgreSQL database.",
			Tags:        []string{"Kubernetes", "AWS SQS", "IAM", "RDS", "Full-Stack", "Multi-Source"},
			Sources: []string{
				"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
				"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0",
				"ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0",
			},
			YAML: strings.TrimSpace(k8sAppYAML),
		},
		{
			ID:          "k8s-workload",
			Name:        "Cloud-Agnostic Web Workload",
			Description: "Zero-dependency cloud-agnostic application composing native Kubernetes Deployment, Service, ConfigMap, and ServiceAccount with full environment & port wiring.",
			Tags:        []string{"Cloud-Agnostic", "Kubernetes", "Native", "ConfigMap", "Deployment"},
			Sources:     []string{},
			YAML:        strings.TrimSpace(k8sWorkloadYAML),
		},
		{
			ID:          "k8s-cronjob",
			Name:        "Cloud-Agnostic Scheduled CronJob",
			Description: "Periodic batch task orchestration composing native Kubernetes CronJob, ConfigMap, and ServiceAccount with cron scheduling and concurrency policies.",
			Tags:        []string{"Cloud-Agnostic", "Kubernetes", "Batch", "CronJob", "Native"},
			Sources:     []string{},
			YAML:        strings.TrimSpace(k8sCronJobYAML),
		},
		{
			ID:          "s3-bucket",
			Name:        "AWS S3 Secure Storage Bucket",
			Description: "Secure AWS S3 Bucket with server-side encryption, versioning configuration, and strict public access block controls.",
			Tags:        []string{"AWS", "S3", "Storage", "Security", "Conditionals"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"},
			YAML:        strings.TrimSpace(s3BucketYAML),
		},
		{
			ID:          "sqs-queue",
			Name:        "AWS SQS Queue with Dead Letter Queue",
			Description: "Resilient AWS SQS messaging topology with Main Queue, Dead Letter Queue (DLQ), and Queue Policy status wire.",
			Tags:        []string{"AWS", "SQS", "Messaging", "Status Wire", "Conditionals"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0"},
			YAML:        strings.TrimSpace(sqsQueueYAML),
		},
	}
}

// Get returns the example with the given id, or an error if not found.
func Get(id string) (*Example, error) {
	for _, ex := range All() {
		if ex.ID == id {
			// Populate ResourceCount dynamically from parsed blueprint
			b, err := blueprint.Parse([]byte(ex.YAML))
			if err == nil && b != nil {
				ex.ResourceCount = len(b.Spec.Resources)
			}
			return &ex, nil
		}
	}
	return nil, fmt.Errorf("example %q not found", id)
}

// List returns summary information for all starter examples with resource counts populated.
func List() []Example {
	exs := All()
	for i := range exs {
		b, err := blueprint.Parse([]byte(exs[i].YAML))
		if err == nil && b != nil {
			exs[i].ResourceCount = len(b.Spec.Resources)
		}
	}
	return exs
}
