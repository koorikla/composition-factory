# Is cloud IAM derivable from a Crossplane managed-resource kind?

Research brief — area: cloud IAM derivation. Date: 2026-08-28.
Every claim is tagged **VERIFIED** (I ran it / read the bytes) or **DOCS** (read it, did not execute).
Working files: `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/`

---

## Decisions this enables

1. **Ship a generated `Kind → Terraform resource` table, do not derive it at runtime.** The mapping is *not* in the provider package (VERIFIED: CRD annotations are empty, `package.yaml` has no such field). It *is* extractable with one regex from 1,350 `apis/cluster/**/zz_<kind>_terraformed.go` files in `crossplane-contrib/provider-upjet-aws` (Apache-2.0). Naming convention alone is only **75.9%** accurate (VERIFIED) — 252 exceptions concentrated in `ec2` (74), `rds` (19), `directconnect` (17). Convention is not good enough to ship.
2. **AWS's CloudFormation registry resource schemas are the dataset.** Public, unauthenticated, AWS-authored, machine-readable, per-resource, per-CRUD-handler IAM action lists, plus a separate `tagging.permissions` block. 1,722 types, **1,577 (91.6%) carry handler permissions** (VERIFIED). Distilled to a permissions-only index: **1.07 MB raw / 101 KB gzipped** — `go:embed`-able.
3. **Automated AWS coverage is ~53% today, ~70% with a bounded curated alias table.** TF-resource → CFN-type matched strictly for 45.8% of the 1,029 upjet-aws resources, +7.4% fuzzy (VERIFIED). The gap is name skew (`aws_rds_cluster` vs `AWS::RDS::DBCluster`), not missing data. Budget a ~180-entry hand alias file, not 1,029.
4. **Label the output "starting point", never "least privilege".** Measured fidelity of CFN permissions against what the Terraform resource actually calls: recall 69–100%, precision 57–94% (VERIFIED on SQS Queue / IAM Role / RDS Cluster / EC2 Instance). It is neither a superset nor a subset. It is honest as a draft; it is a liability if presented as authoritative.
5. **Read/tagging split is free, and you must patch one systematic hole.** The CFN schema separates `read` from `create/update/delete` and has a dedicated `tagging.permissions` array — that maps 1:1 onto upjet's Observe-loop needs. But CFN's `read` handler systematically omits tag-read actions (VERIFIED: `AWS::RDS::DBCluster` read = `rds:DescribeDBClusters` only, while Terraform's `rds/tags_gen.go` calls `ListTagsForResource` on every read). **Always union `tagging.permissions` into the read set.**

---

## 1. The upjet → Terraform chain

### (a) Is the TF resource name recorded in the provider package? **NO.**

VERIFIED, by pulling and unpacking `xpkg.upbound.io/upbound/provider-aws-sqs:v2` (digest `sha256:e3aaedcc…`) and reading every layer:

| Location | Result |
|---|---|
| `CRD .metadata.annotations` (`queues.sqs.aws.m.upbound.io`, live cluster) | `{}` — empty |
| `CRD .metadata.labels` | `{}` — empty |
| CRD `names.categories` | `[crossplane, managed, aws]` — no TF hint |
| `package.yaml` `meta.pkg.crossplane.io/v1 Provider` annotations | `auth.upbound.io/group`, `friendly-name`, `description`, `license`, `maintainer`, `readme`, `source`, `hardening`, `host`, `support`, `verification` — **no terraform field** |
| `models/*.schema.json` (18 files) | JSON schema only |
| Crossplane v2 `ManagedResourceDefinition` `.spec` | `[conversion, group, names, scope, state, versions]` — no permissions/TF field |
| `/usr/local/bin/provider` (967 MB) | `aws_sqs_queue` occurs **110×** — compiled Go string data from `GetTerraformResourceType()`, not declaratively extractable |

The 9 textual `aws_sqs_queue` hits inside the CRD are *incidental* — they sit in field descriptions copied verbatim from Terraform registry docs ("It is preferred to use the `aws_sqs_queue_policy` resource instead"). They reference *sibling* resources, not the resource itself. Parsing those would be actively wrong.

**Negative result, stated plainly: a compositionfactory instance that only has the installed CRDs cannot recover the Terraform resource name.**

### (b) Is it derivable from Kind + group by convention? **75.9% — not good enough.**

VERIFIED. Method: pulled the full git tree of `crossplane-contrib/provider-upjet-aws@main` (22,725 entries, untruncated) via the GitHub trees API; took the 1,045 unique `(group, kind)` pairs implied by `examples/<group>/<scope>/<version>/<kind>.yaml`; tested `"aws_" + group + "_" + snake(Kind)` against the 1,029 names in `config/generated.lst`.

```
Kind+group -> TF name by pure convention:  793/1045 = 75.9%
exceptions:                                252     = 24.1%
top exception groups: ec2(74) rds(19) directconnect(17) cloudwatchlogs(11)
                      cognitoidp(9) kafka(9) neptune(9) cloudwatchevents(8)
                      elb(8) elbv2(8) configservice(7) dynamodb(5)
```
(A handful of the 252 are example-filename artifacts like `apigateway/stage-2`, so true convention accuracy is nearer 80% — still not shippable.)

Canonical failures: `ec2/Instance → aws_instance` (not `aws_ec2_instance`), `rds/Cluster → aws_rds_cluster` but `rds/Instance → aws_db_instance`, `elbv2/LB → aws_lb`.

### (c) What *is* authoritative and machine-readable

VERIFIED: `apis/cluster/<group>/<version>/zz_<kind>_terraformed.go` (**1,350 files**, Apache-2.0) each contain exactly:

```go
// GetTerraformResourceType returns Terraform resource type for this Queue
func (mg *Queue) GetTerraformResourceType() string {
	return "aws_sqs_queue"
}
```

`crossplane-contrib/provider-upjet-gcp@main` has the identical layout — **820** `*_terraformed.go` files (VERIFIED via its git tree).

**Recommendation:** a `go:generate` step that walks these paths and emits `map[GroupKind]string`. Path gives group+kind, regex gives the TF name. One pass, no heuristics, refreshable per provider release.

Also useful, both in-repo and machine-readable: `config/generated.lst` (JSON array, **1,029** TF resource names for AWS, **406** for GCP), `config/schema.json`, `config/provider-metadata.yaml`, `config/externalname.go`.

---

## 2. Terraform → IAM actions: what exists

### 2.1 CloudFormation registry resource schemas — **the answer** ⭐

**Source (VERIFIED, HTTP 200, no auth):** `https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip` → 2.9 MB zip, 13.9 MB unpacked, **1,722** `.json` files.

Real sample, `aws-sqs-queue.json` (verbatim):

```json
"handlers": {
  "create": {"permissions": ["sqs:CreateQueue","sqs:GetQueueUrl","sqs:GetQueueAttributes","sqs:ListQueueTags","sqs:TagQueue"]},
  "read":   {"permissions": ["sqs:GetQueueAttributes","sqs:ListQueueTags"]},
  "update": {"permissions": ["sqs:SetQueueAttributes","sqs:GetQueueAttributes","sqs:ListQueueTags","sqs:TagQueue","sqs:UntagQueue"]},
  "delete": {"permissions": ["sqs:DeleteQueue","sqs:GetQueueAttributes"]},
  "list":   {"permissions": ["sqs:ListQueues"]}
},
"tagging": {"taggable": true, "tagOnCreate": true, "tagUpdatable": true,
            "tagProperty": "/properties/Tags",
            "permissions": ["sqs:TagQueue","sqs:UntagQueue","sqs:ListQueueTags"]}
```

Coverage measured (VERIFIED):

| metric | count | % of 1,722 |
|---|---:|---:|
| schemas total | 1,722 | 100% |
| with any `handlers` | 1,577 | 91.6% |
| with **all four** of create/read/update/delete permissions | 1,401 | 81.4% |
| with `tagging.permissions` | 1,068 | 62.0% |

Missing-handler examples: `AWS::AppMesh::Mesh`, `AWS::Pinpoint::App`, `AWS::Glue::Partition`, `AWS::MediaLive::Channel`, `AWS::ElastiCache::SecurityGroup` — all legacy (pre-registry) types.

Bonus: handler permissions **include cross-service dependencies**, which is exactly what breaks real deployments. `AWS::Athena::WorkGroup` create needs `s3:*` + `kms:*` + `iam:*`; `AWS::EC2::Instance` needs `iam:PassRole` and `ssm:*`; `AWS::RDS::DBCluster` needs `secretsmanager:CreateSecret` and `iam:CreateServiceLinkedRole`. No naming heuristic would ever produce those.

### 2.2 AWS Service Reference Information — official access-level flags ⭐

**Source (VERIFIED, HTTP 200, no auth):**
- index: `https://servicereference.us-east-1.amazonaws.com/` → JSON array, **455 services**
- per service: `https://servicereference.us-east-1.amazonaws.com/v1/sqs/sqs.json`

Shape (verbatim):
```json
{"Name":"sqs","Actions":[
  {"Name":"AddPermission",
   "Annotations":{"Properties":{"IsList":false,"IsPermissionManagement":true,
                                "IsTaggingOnly":false,"IsWrite":true}},
   "Resources":[{"Name":"queue"}],
   "SupportedBy":{"IAM Access Analyzer Policy Generation":true,
                  "IAM Action Last Accessed":true}}, …],
 "ConditionKeys":[…],"Operations":[…],"Resources":[…],"Version":…}
```

This is **the official, AWS-maintained, machine-readable Service Authorization Reference** — it replaces HTML scraping. It carries the access-level classification as booleans (`IsWrite` / `IsList` / `IsPermissionManagement` / `IsTaggingOnly`; Read = not write, not list). It does **not** map to Terraform.

### 2.3 `iann0036/iam-dataset` — MIT, excellent, but SDK-keyed not TF-keyed

VERIFIED by cloning (`aws/` 113 MB, `gcp/` 82 MB, `azure/` 112 MB; **LICENSE = MIT, © 2021 Ian Mckay**).

| file | shape | size/coverage |
|---|---|---|
| `aws/iam_definition.json` | list of 455 services, each `{prefix, service_name, privileges[], resources[], conditions[]}`; each privilege has `access_level` ∈ {Read, Write, List, Tagging, Permissions management} (+ combos), `resource_types[]` with `condition_keys` and `dependent_actions` | 12.4 MB, **21,820 actions** (VERIFIED counts) |
| `aws/map.json` | `sdk_method_iam_mappings`: **19,514** SDK methods → IAM actions, with ARN templates; plus SDK↔service and CloudTrail↔service maps | 7.4 MB |
| `aws/tags.json`, `aws/managed_policies.json`, `aws/access_level_overrides.json`, `aws/docs.json` | supporting | — |
| `gcp/permissions.json` | **10,129** permissions → list of predefined roles containing them | — |
| `gcp/map.json` | GCP API method → permissions (README marks it **WORK IN PROGRESS**) | — |
| `gcp/predefined_roles.json`, `gcp/role_permissions.json`, `gcp/roles/*.json` | full predefined-role corpus | — |

`map.json` sample (VERIFIED) — 23 SQS methods, e.g. `SQS.CreateQueue → [sqs:CreateQueue, sqs:TagQueue]`.

**Key negative result: there is no Terraform-resource key anywhere in `iam-dataset`.** The chain it supports is *SDK method → IAM action*. To use it for a Crossplane MR you would first have to determine which SDK methods the Terraform resource calls — which is exactly the missing link.

### 2.4 iamlive — dynamic, MIT, not a static TF map

VERIFIED (GitHub API): `iann0036/iamlive`, **MIT**, 3,408 stars, "Generate an IAM policy from AWS, Azure, or Google Cloud (GCP) calls using client-side monitoring (CSM) or embedded proxy."

DOCS: iamlive works by proxying live SDK traffic (`HTTP_PROXY`/`HTTPS_PROXY`/`AWS_CA_BUNDLE`) and resolving observed calls through `iam-dataset`'s `map.json`. It ships **no static Terraform-resource → IAM mapping**. It requires you to actually run `terraform apply` against real AWS. **Unusable for a design-time canvas.** Same structural objection applies to `trailscraper` and any CloudTrail-derived approach: they need the resource to already exist.

### 2.5 `salesforce/policy_sentry` — MIT, same SAR data, no TF layer

VERIFIED by cloning. `policy_sentry/shared/data/iam-definition.json` — 62 MB data dir, a **dict of 452 services** keyed by prefix, each privilege carrying `access_level`, `resource_types` (with `required`, `condition_keys`, `dependent_actions`) and `api_documentation_link`. LICENSE text is MIT-form ("Permission is hereby granted, free of charge… © 2019 Salesforce.com").

This is materially the same SAR corpus as `iam-dataset/aws/iam_definition.json`, restructured. **It has no Terraform or CloudFormation resource key.** Its value to compositionfactory is exclusively the access-level taxonomy (see §6) — and AWS's own Service Reference (§2.2) now supplies that with a better provenance story.

### 2.6 Terraform AWS provider source — declares nothing about IAM ⭐ negative result

VERIFIED. `internal/service/sqs/queue.go` (655 lines) contains no IAM metadata whatsoever; the *only* signal is the literal SDK calls:

```
internal/service/sqs/queue.go     → conn.CreateQueue, conn.DeleteQueue,
                                    conn.GetQueueAttributes, conn.SetQueueAttributes
internal/service/sqs/tags_gen.go  → conn.ListQueueTags, conn.TagQueue, conn.UntagQueue
```

Extracting this is Go static analysis over ~200 service packages, complicated by paginator constructors (`sqs.NewListXPaginator(conn, …)`) that never appear as `conn.X(`. Feasible as a research project; **not** feasible as a feature in a node-graph editor.

DOCS: `hashicorp/terraform-provider-aws` issue **#32823** ("Generate a list of least permissions required to provision a stack") is an **open enhancement request** — i.e. upstream confirms this does not exist.

**What the provider *does* ship, and it is valuable:** `names/data/names_data.hcl` (MPL-2.0, 203 KB, VERIFIED fetched). 373 `service` blocks + 9 `sub_service` blocks, each with:

```hcl
service "sqs" {
  sdk { id = "SQS"  arn_namespace = "sqs" }
  resource_prefix { correct = "aws_sqs_" }
}
service "rds" {
  sdk { id = "RDS"  arn_namespace = "rds" }
  resource_prefix { actual = "aws_(db_|rds_)"  correct = "aws_rds_" }
}
service "ec2" {
  sdk { id = "EC2"  arn_namespace = "ec2" }
  resource_prefix {
    actual = "aws_(ami|availability_zone|ec2_(…)|eip|instance|key_pair|launch_template|placement_group|spot)"
    correct = "aws_ec2_" }
}
```

`resource_prefix.actual` is a **regex**, and `arn_namespace` is exactly the IAM action prefix. Measured (VERIFIED):

```
TF resource -> IAM service prefix via names_data.hcl regexes: 1027/1029 = 99.8%
unmapped: ['aws_lb', 'aws_route']   (regex anchoring; 2 hand entries)
```

So **"which AWS service does this MR talk to" is a solved, 99.8%-mechanical problem.** Only "which actions within that service" is hard.

### 2.7 Other candidates, checked and dismissed

- **cloudsplaining** (Salesforce) — analyses *existing* policies for over-permission. Consumes policies, does not produce them from resource types. Not applicable.
- **`iann0036/aws-leastprivilege`** — DOCS: CloudFormation-driven, uses live CloudTrail. Same design-time objection as iamlive.
- **`udondan/iam-floyd`** — generated from SAR; a *policy-authoring* library, no resource-type index. Duplicates §2.2.
- **`aws-actions/terraform-aws-iam-policy-validator`** — validates policies *found inside* Terraform templates via IAM Access Analyzer. Solves a different problem entirely.
- **Upbound / Crossplane themselves — publish nothing.** VERIFIED negative: no permissions field in the package, MRD, or CRD; searching Upbound docs finds only the IRSA setup page saying "attach the necessary AWS permissions to this role", with no per-resource breakdown.

### 2.8 GCP

- **`gcloud iam list-testable-permissions`** — DOCS: runtime, authenticated, keyed by an existing resource URL. Not usable at design time.
- **`iam-dataset/gcp/permissions.json`** (MIT) — VERIFIED: 10,129 permissions → containing roles. The *minimum predefined role* is computable by set intersection. Demo (VERIFIED, executed):
  ```
  pubsub.topics.create → 25 roles;  .get → 41;  .update → 18;  .delete → 20;  .getIamPolicy → 12
  roles granting ALL five: roles/pubsub.admin, roles/owner,
                           + 5 service-agent roles (clouddeploymentmanager, cloudtpu,
                             composer, dataflow, dlp)
  ```
  Filtering out `roles/owner` and `*serviceAgent` yields `roles/pubsub.admin` — clean, mechanical, correct.
- **Naive name convention** `google_<svc>_<res>` → `<svc>.<plural(res)>.{create,get,update,delete}` measured over the 406 `provider-upjet-gcp` TF resources (VERIFIED):
  ```
  full  (>=3 of 4 CRUD verbs resolve to real permissions): 165  40.6%
  partial (1-2 verbs):                                       4   1.0%
  no match:                                                237  58.4%
  ```
- **Magic Modules is the GCP equivalent of the CFN schema — better than the naive guess.** VERIFIED fetched, Apache-2.0:
  - `mmv1/products/pubsub/product.yaml` → `versions: [{name: ga, base_url: https://pubsub.googleapis.com/v1/}]`
  - `mmv1/products/pubsub/Topic.yaml` → `base_url: projects/{{project}}/topics`, `update_url: …/topics/{{name}}`, `create_verb: PUT`, `update_verb: PATCH`
  
  `pubsub` + collection `topics` + verbs → `pubsub.topics.{create,get,update,delete}` deterministically. `terraform-provider-google` is generated from these files, so the API surface is exact by construction rather than guessed. **This is the right GCP source, and it is not the same class of guess as the AWS name-matching problem.** (Not implemented/measured here — flagged as the strongest untested lead.)

---

## 3. Coverage test — 5 concrete resources

All permission sets below are **verbatim from the CFN schemas** (VERIFIED, no editing). "TF actual" is from grepping `conn.<Method>(` in `hashicorp/terraform-provider-aws@main` (VERIFIED; a *lower bound* — paginator-based List calls do not match this pattern).

### `sqs.aws.m.upbound.io/Queue` — `aws_sqs_queue` → `AWS::SQS::Queue`
```
create   sqs:CreateQueue GetQueueUrl GetQueueAttributes ListQueueTags TagQueue
read     sqs:GetQueueAttributes ListQueueTags
update   sqs:SetQueueAttributes GetQueueAttributes ListQueueTags TagQueue UntagQueue
delete   sqs:DeleteQueue GetQueueAttributes
tagging  sqs:TagQueue UntagQueue ListQueueTags
```
TF actual (7): CreateQueue, DeleteQueue, GetQueueAttributes, SetQueueAttributes, ListQueueTags, TagQueue, UntagQueue.
**recall 100%, precision 88%** — only `sqs:GetQueueUrl` is surplus. *100% mechanical.*

### `iam.aws.m.upbound.io/Role` — `aws_iam_role` → `AWS::IAM::Role`
17 CRUD actions + tagging `iam:TagRole UntagRole ListRoleTags`.
**recall 89%, precision 94%.** CFN **misses** `iam:RemoveRoleFromInstanceProfile` and `iam:ListInstanceProfilesForRole` — both of which Terraform calls on delete when `force_detach_policies` is set. *Mechanical, with a known 2-action hole.*

### `rds.aws.m.upbound.io/Cluster` — `aws_rds_cluster` → `AWS::RDS::DBCluster`
28 actions across `rds`, `iam`, `ec2`, `secretsmanager`.
**recall 87%, precision 57%.** CFN **misses `rds:ListTagsForResource`** (VERIFIED present in `internal/service/rds/tags_gen.go`) and `rds:PromoteReadReplicaDBCluster`. CFN **over-grants** `rds:CreateDBInstance / DeleteDBInstance / ModifyDBInstance / CreateDBClusterSnapshot / DescribeDBSnapshots / …` because CFN's DBCluster handler also manages instances; Terraform's does not. *Mechanical + judgement — this is the case that proves you must show the user the diff.*

### `ec2.aws.m.upbound.io/Instance` — `aws_instance` → `AWS::EC2::Instance`
36 actions across `ec2`, `iam` (`iam:PassRole`), `ssm`.
**recall 69%, precision 65%** — the worst of the five. CFN misses `ec2:GetPasswordData`, `DescribeTags`, `ModifyVolume`, `Assign/UnassignPrivateIpAddresses`, `ModifyInstanceCpuOptions`, `ModifyNetworkInterfaceAttribute`, `ModifyInstanceCapacityReservationAttributes`, `CancelSpotInstanceRequests`. CFN adds `ssm:*` and a batch of `Describe*` that Terraform resolves elsewhere. *Also note the TF name `aws_instance` is one of the 86 legacy names that need a curated entry.*

### `s3.aws.m.upbound.io/Bucket` — `aws_s3_bucket` → `AWS::S3::Bucket` — **structural mismatch**
CFN union CRUD = **71 actions** across `s3`, `s3tables`, `iam`. This is a *category error*: `AWS::S3::Bucket` is a mega-resource covering versioning, replication, lifecycle, notifications, CORS, logging, inventory, metrics, ownership controls and object lock, whereas Terraform (provider v4+) split those into ~20 separate resources (`aws_s3_bucket_versioning`, `aws_s3_bucket_lifecycle_configuration`, …). An MR of Kind `Bucket` would be handed 71 actions when it needs roughly `s3:CreateBucket, DeleteBucket, ListBucket, GetBucketTagging, PutBucketTagging, GetBucketAcl`.
**Judgement required. This is the failure mode the UI must be honest about.** Mitigation: flag any resource whose CFN action count exceeds ~2× the service median as "over-broad, review".

**Summary of the five:** 4 of 5 mechanical to a reviewable draft; 1 (S3) actively misleading without human review. Mean recall ≈ 86%, mean precision ≈ 76%.

---

## 4. Honest verdict

### **(b) — derivable as an approximate starting point a human must review.** Not (a), not (c).

Rejecting (a) *high fidelity*: measured recall 69–100% and precision 57–94% against actual Terraform call sites. Both directions leak. Every one of the four comparable resources had at least one wrong action. Nothing in the pipeline can be called least-privilege.

Rejecting (c) *hand-curation only*: over half the corpus resolves to a real, AWS-authored, cross-service-aware action list with zero human input. Hand-curating 1,029 resources would be worse *and* staler than this.

### Quantified AWS coverage (VERIFIED, tiered over all 1,029 `provider-upjet-aws` TF resources)

| tier | what the user sees | count | % |
|---|---|---:|---:|
| **T1** CFN type matched strictly, has handler perms | exact per-resource CRUD + tagging action list | 471 | **45.8%** |
| **T1b** CFN type matched by suffix fuzz, has perms | same, flag "name-matched heuristically" | 76 | **7.4%** |
| **T2** CFN type matched but schema has no handlers | fall through to T3/T5 | 61 | 5.9% |
| **T3** SAR verb heuristic finds both Create*+Delete* | plausible CRUD set, "unverified" | 84 | 8.2% |
| **T4** SAR verb heuristic finds some verbs | partial set, "incomplete" | 35 | 3.4% |
| **T5** service known, no resource-level match | `sqs:*`-style service scope + access-level filter | 178 | 17.3% |
| **T0** TF name has no service prefix (legacy `aws_instance`, `aws_vpc`, `aws_db_*`, `aws_lb*`, `aws_cognito_*`) | nothing without a curated entry | 86 | 8.4% |
| **T6** nothing at all | nothing | 38 | 3.7% |

**Headline: 53.2% (T1+T1b) get a real per-resource action set with zero curation.**

The T0 set is 86 names and *almost all of them have obvious CFN counterparts* (`aws_instance`→`AWS::EC2::Instance`, `aws_vpc`→`AWS::EC2::VPC`, `aws_db_instance`→`AWS::RDS::DBInstance`, `aws_lb`→`AWS::ElasticLoadBalancingV2::LoadBalancer`, `aws_cognito_user_pool`→`AWS::Cognito::UserPool`). Adding those plus ~100 CFN-name aliases (`cluster`↔`DBCluster`, `plan`↔`BackupPlan`, `policy`↔`ScalingPolicy`) is a **bounded ~180-line hand file** that should lift T1 coverage to roughly **70%** *(estimate — extrapolated from the miss list, not measured)*. The remaining ~30% are genuinely CFN-less: sub-resources and association/attachment types that CloudFormation models as properties of a parent (`aws_appstream_fleet_stack_association`, `aws_autoscaling_attachment`, `aws_backup_vault_policy`, `aws_api_gateway_method_settings`).

**GCP:** 40.6% by naive convention (VERIFIED). Magic Modules should beat that substantially but is **untested** — do not promise a number.

### What the UI must therefore do
- Print provenance per resource: *"AWS CloudFormation resource schema, AWS::SQS::Queue"* vs *"heuristic, unverified"* vs *"service scope only"*.
- Never emit a policy without a visible confidence tier.
- Emit `Resource: "*"` with a warning rather than fabricating ARNs. (`iam_definition.json` carries ARN templates per resource type if you later want to narrow them — MIT.)
- Flag over-broad cases (S3-style mega-resources).

---

## 5. Licensing

| dataset | licence | can an MIT/Apache Go tool use it? |
|---|---|---|
| **CFN registry schemas** (`schema.cloudformation.*.amazonaws.com/CloudformationSchema.zip`) | No licence file in the zip (VERIFIED: 1,722 files, none carries a header). Each schema's `sourceUrl` points at `github.com/aws-cloudformation/aws-cloudformation-resource-providers-<svc>`, which are **Apache-2.0** (DOCS). | **Yes, with care.** Safest: fetch at *build* time in `go:generate` and cache, or fetch at runtime with a bundled fallback — rather than vendoring the zip verbatim. Extracting only `{typeName → {op → [actions]}}` is factual data (action strings), the least copyrightable part. |
| **AWS Service Reference** (`servicereference.us-east-1.amazonaws.com`) | AWS service data over a public endpoint, no licence file | **Yes.** Same fetch-don't-vendor posture. Best provenance of any option — it is AWS's own current publication. |
| **`iann0036/iam-dataset`** | **MIT** (VERIFIED: `LICENSE`, © 2021 Ian Mckay) | **Yes**, including redistribution, with attribution. |
| **`salesforce/policy_sentry`** | MIT-form (VERIFIED: "Permission is hereby granted, free of charge…", © 2019 Salesforce.com) | Yes — but it adds nothing over the two above. |
| **`hashicorp/terraform-provider-aws` `names/data/names_data.hcl`** | **MPL-2.0** (VERIFIED: `SPDX-License-Identifier: MPL-2.0` header) | **Care needed.** MPL-2.0 is *file-level* copyleft: shipping the file (or a modified copy) keeps that file under MPL and obliges source availability for it. An MIT/Apache Go binary can link to it, but a vendored derived table is arguably a modified form. **Cleanest path: derive the same TF-prefix → IAM-prefix table from `iam-dataset` (MIT) service prefixes plus `config/generated.lst`, and use `names_data.hcl` only to spot-check.** Alternatively isolate the derived table in its own MPL-licensed file with the header preserved. |
| **`crossplane-contrib/provider-upjet-*`** (`generated.lst`, `zz_*_terraformed.go`) | **Apache-2.0** (VERIFIED: `meta.crossplane.io/license: Apache-2.0` in package.yaml; repo licence Apache-2.0) | **Yes**, with NOTICE attribution. |
| **`GoogleCloudPlatform/magic-modules`** | **Apache-2.0** (VERIFIED: header in `mmv1/products/pubsub/product.yaml`) | **Yes**, with attribution. |
| **`iann0036/iamlive`** | MIT (VERIFIED via GitHub API) | Yes, but it is a runtime tool, not a dataset. |

**Size:** distilled CFN permissions-only index (typeName → {create,read,update,delete,list,tagging} → actions) = **1,577 entries, 1,070,392 bytes raw, 103,720 bytes gzipped** (VERIFIED, generated). Trivially `go:embed`-able.

---

## 6. The read side, and tags

Upjet reconciles by calling the Terraform Read/Refresh path on **every** reconcile loop for **every** MR — so read permissions are hit continuously, not just at creation. Getting them wrong is the silent failure ("resource stays `Synced=False` / flaps") rather than a loud create error.

**Does any dataset capture the read/write distinction? Yes, three of them, at two different granularities:**

1. **CFN schemas — per resource, per operation.** `handlers.read.permissions` is exactly the drift-detection set, and `handlers.list` is separate. This is the *only* source that gives the split **per resource type**. Use it.
2. **AWS Service Reference — per action, official flags.** `IsWrite`, `IsList`, `IsPermissionManagement`, `IsTaggingOnly` (VERIFIED on `sqs.json`). Read = `!IsWrite && !IsList`.
3. **`iam-dataset` / `policy_sentry` — per action, 5-level SAR taxonomy.** VERIFIED distribution for SQS's 20 privileges: `Write 7, Read 7, Permissions management 3, "Tagging, Write" 2, "Permissions management, Write" 1`. Note actions can carry *combined* levels — treat `access_level` as a set, not an enum.

**Tagging.** `tagging.permissions` is a first-class, separate array in the CFN schema (1,068 of 1,722 types have it, VERIFIED), and it correctly includes the *read* tag action (`sqs:ListQueueTags`, `iam:ListRoleTags`, `s3:ListTagsForResource`).

**The systematic hole you must patch — VERIFIED:**
`AWS::RDS::DBCluster` `handlers.read.permissions == ["rds:DescribeDBClusters"]`, and its `tagging.permissions == ["rds:AddTagsToResource","rds:RemoveTagsFromResource"]` — **`rds:ListTagsForResource` appears in neither**, yet `internal/service/rds/tags_gen.go` calls `ListTagsForResource` on every read. An MR built from CFN's read set alone will fail tag drift detection.

**Rule to implement:** `read_set = handlers.read.permissions ∪ tagging.permissions`, and additionally, for any service where the SAR/Service-Reference data contains an action matching `^List.*Tags|^ListTagsFor|^GetResources$` with `IsWrite == false`, add it. That closes the RDS class of hole mechanically.

**Baseline permissions, independent of resource kind (VERIFIED):** `hashicorp/aws-sdk-go-base` calls `stsClient.GetCallerIdentity` during provider configuration (`awsauth.go:179`, `getAccountIDAndPartitionFromSTSGetCallerIdentity`). So **`sts:GetCallerIdentity` is required by every upjet-AWS provider regardless of what is on the canvas** (unless `skip_requesting_account_id` is set), plus `sts:AssumeRoleWithWebIdentity` when authenticating via IRSA (DOCS — Upbound IRSA setup page).

---

## Recommended pipeline for compositionfactory

```
MR (group, Kind)
  └─(1) generated table from zz_<kind>_terraformed.go        Apache-2.0, exact, 1,350 entries
        → terraform resource name (aws_sqs_queue)
        └─(2a) generated alias table + name normalisation     ~180 hand entries
              → CFN typeName (AWS::SQS::Queue)
              └─ CFN schema handlers + tagging                AWS, ~53% now / ~70% w/ aliases
                 → per-op action sets  [confidence: HIGH]
        └─(2b) fallback: names_data.hcl regexes / iam-dataset  99.8% service resolution
              → IAM service prefix (sqs)
              └─ Service Reference actions filtered by
                 IsWrite/IsList/IsTaggingOnly + verb+noun match
                 → candidate action set  [confidence: LOW — label it]
  + always: sts:GetCallerIdentity, and ∪ tagging.permissions into the read set
```

GCP: same shape, substituting Magic Modules `product.yaml`+`<Resource>.yaml` for the CFN schema, and `iam-dataset/gcp/permissions.json` set-intersection for "smallest predefined role".

## Artefacts produced (in scratchpad, reusable)
- `cfnschema/` — 1,722 unpacked CFN schemas
- `distilled_cfn_perms.json[.gz]` — 1,577-entry permissions-only index (1.07 MB / 101 KB)
- `generated.lst`, `gcp_generated.lst` — 1,029 / 406 upjet TF resource names
- `tf2iamsvc.json` — TF resource → IAM service prefix, 1,027/1,029
- `tiers.json`, `mapreport*.txt`, `kindmap.txt`, `five.txt` — all measurements above
- `svcref.json`, `svcref_sqs.json` — AWS Service Reference index + SQS sample
- `iam-dataset/`, `ps/` — cloned MIT datasets

## Sources
- [AWS CloudFormation registry schemas (zip)](https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip)
- [AWS Service Reference Information](https://servicereference.us-east-1.amazonaws.com/)
- [AWS Service Authorization Reference (HTML)](https://docs.aws.amazon.com/service-authorization/latest/reference/reference.html)
- [iann0036/iam-dataset](https://github.com/iann0036/iam-dataset) · [MAP-README](https://github.com/iann0036/iam-dataset/blob/main/aws/MAP-README.md)
- [iann0036/iamlive](https://github.com/iann0036/iamlive)
- [salesforce/policy_sentry](https://github.com/salesforce/policy_sentry)
- [crossplane-contrib/provider-upjet-aws](https://github.com/crossplane-contrib/provider-upjet-aws) · [config/generated.lst](https://raw.githubusercontent.com/crossplane-contrib/provider-upjet-aws/main/config/generated.lst)
- [crossplane-contrib/provider-upjet-gcp](https://github.com/crossplane-contrib/provider-upjet-gcp)
- [hashicorp/terraform-provider-aws names_data.hcl](https://raw.githubusercontent.com/hashicorp/terraform-provider-aws/main/names/data/names_data.hcl)
- [terraform-provider-aws#32823 — generate least permissions (open)](https://github.com/hashicorp/terraform-provider-aws/issues/32823)
- [GoogleCloudPlatform/magic-modules](https://github.com/GoogleCloudPlatform/magic-modules)
- [Upbound AWS IRSA authentication](https://docs.upbound.io/manuals/packages/providers/aws-auth/aws-irsa/)
- [Streamlining IAM permission discovery with CloudFormation resource schemas](https://dev.to/quixoticmonk/streamlining-iam-permission-discovery-with-cloudformation-resource-schemas-4lpg)
