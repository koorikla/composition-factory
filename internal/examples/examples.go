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
			Description: "IAM Role with scoped AssumeRole trust policy, S3 permissions policy, and native K8s ServiceAccount with role-arn status annotation wire.",
			Tags:        []string{"AWS", "IAM", "Kubernetes", "Status Wire", "Annotations"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0"},
			YAML:        strings.TrimSpace(irsaYAML),
		},
		{
			ID:          "rds-postgres",
			Name:        "AWS RDS PostgreSQL Database",
			Description: "RDS DB Instance with configurable storage, instance class, engine version, credentials connection secret envelope, and multi-AZ.",
			Tags:        []string{"AWS", "RDS", "Database", "Envelopes", "Parameters"},
			Sources:     []string{"ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0"},
			YAML:        strings.TrimSpace(rdsYAML),
		},
		{
			ID:          "k8s-app",
			Name:        "Full-Stack Microservice (App + SQS + IRSA)",
			Description: "Kubernetes Deployment & Service wired to an AWS SQS queue and an IAM Role assumed via ServiceAccount IRSA annotation.",
			Tags:        []string{"Kubernetes", "AWS SQS", "IAM", "Full-Stack", "Multi-Source"},
			Sources: []string{
				"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
				"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0",
			},
			YAML: strings.TrimSpace(k8sAppYAML),
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
