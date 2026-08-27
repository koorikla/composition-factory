# Prior Art: Permission Generation, and How Tools Present It

**Area:** How other tools generate and present required permissions, so compositionfactory does not reinvent and presents it usefully.

**Method note.** **[VERIFIED]** = I ran it against the live `kind-platform` cluster (Crossplane v2.4.0 / CLI v2.5.0), executed the binary, or parsed the raw data file myself. **[DOCS]** = read from vendor docs or a project README, not executed. Negative results are called out explicitly as **[NOT DERIVABLE]**. Data pulled 2026-08-28.

---

## Decisions this enables

1. **Ship two control-plane artifacts, not one, and do not try to put either inside the Configuration package.** **[VERIFIED]** `crossplane xpkg build` hard-fails on a package containing a ClusterRole: `no kind "ClusterRole" is registered for version "rbac.authorization.k8s.io/v1" in scheme`. **[VERIFIED]** There is also no `permissionRequests` field anywhere on `configurations/configurationrevisions/providerrevisions.pkg.crossplane.io` in v2.4.0 — I dumped all three CRD schemas and the permission-key list is empty for every served version. So the permissions artifact is necessarily a **sibling file applied out-of-band** (GitOps/Helm/kubectl), never a package payload. Plan the UX around "here are files to commit", not "it ships with your Configuration".

2. **The Kubernetes RBAC side is a solved derivation with a free correctness oracle — that is compositionfactory's unfair advantage.** GVKs are known at design time, and `SubjectAccessReview` answers "is this already granted?" for free. **[VERIFIED]** Against the crossplane SA, 5 of 17 common composable kinds are already permitted (`configmaps`, `secrets`, `services`, `serviceaccounts`, `deployments.apps`) and 12 are not (`statefulsets`, `jobs`, `cronjobs`, `ingresses`, `networkpolicies`, `persistentvolumeclaims`, `horizontalpodautoscalers`, `roles`, `rolebindings`, `poddisruptionbudgets`, `namespaces`, `pods`). **No other tool in this survey can verify its own output against the target system.** Build that check in; it converts "inferred" into "verified" and kills the trust problem for the k8s half.

3. **For AWS control-plane IAM, use the CloudFormation Registry as the action source — it is AWS-authored, machine-readable, and free.** **[VERIFIED]** `https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip` yields 1,722 schemas; 1,577 (91.6%) carry per-lifecycle-handler IAM action lists across 9,033 distinct actions (create 1,577 / read 1,569 / update 1,410 / delete 1,570 / list 1,495). `AWS::SQS::Queue` gives exactly `create: [sqs:CreateQueue, sqs:GetQueueUrl, sqs:GetQueueAttributes, sqs:ListQueueTags, sqs:TagQueue]`, etc. **But the join is the weak link:** **[VERIFIED]** naive `AWS::{service}::{Kind}` mapping from upjet MR kinds hits only 2 of 4 real SQS kinds — `Queue` and `QueuePolicy` hit; `QueueRedrivePolicy` and `QueueRedriveAllowPolicy` miss because Terraform splits resources CloudFormation folds into attributes. Budget for a curated override table and an explicit "unmapped" state.

4. **Steal `Sid` for IAM attribution and one-rule-per-node for RBAC attribution — both are free.** **[VERIFIED/DOCS]** `policy_sentry` and `aws-leastprivilege` independently converged on encoding provenance in the `Sid` field (`SsmReadParameter`, `LambdaFunction-Create1`). For RBAC there is no such field, but **Kubernetes RBAC rules are a pure additive union with no deny**, so emitting one un-merged rule per node is semantically identical to a merged role and preserves attribution. **[VERIFIED]** `controller-gen` — the canonical static RBAC generator — merges and alphabetizes, and its output contains **zero** trace of which marker produced which rule. That is the gap to fill, and it costs nothing.

5. **Copy IAM Access Analyzer's confidence tiering verbatim; it is the only battle-tested design for this exact problem.** **[DOCS]** It splits output into "Policy with action-level information" (confident) versus a separate "Services used" list where "information about which actions were used might not be available… use the menus for each service listed to manually choose the actions". It leaves **resource ARN placeholders** as visible holes and publishes its known blind spots (`iam:PassRole` "is not tracked by CloudTrail and is not included in generated policies"). **[VERIFIED]** `iann0036/iam-dataset` independently does per-entry provenance with a 4-value taxonomy — `manual` (3,849), `restcrawliamblockv1` (2,605), `restcrawlv1` (1,828), `fuzzv1` (18) — and entries carrying two methodologies are corroborated. Adopt a 3-tier `verified / inferred / unknown` model and **never silently omit an unknown**.

---

## 1. Terraform / AWS ecosystem

### 1.1 The runtime-observation family (wrong shape for us)

| Tool | Input | Output | Notes |
|---|---|---|---|
| `iamlive` | Live AWS/Azure/GCP API calls via CSM or MITM proxy | IAM policy JSON, streamed | **[DOCS]** Requires *running* the thing. `--fails-only`, `--force-wildcard-resource`, `--override-aws-map`. Azure/GCP marked "in preview and may produce incorrect outputs at this time." |
| `iamzero` | Application/CLI **errors** at runtime | Least-privilege recommendation | **[DOCS]** Reactive — matches access-denied errors against advisory lists. AWS only. Effectively abandoned. |
| IAM Access Analyzer policy generation | Up to 90 days of CloudTrail | Policy template | **[DOCS]** AWS-native. Best-in-class *presentation* (see §4/§5). |
| `airiam` | Live account IAM usage | Least-privilege Terraform | **[DOCS]** Rightsizes existing principals; not design-time. |

**All of these require the resource to already exist and be exercised.** compositionfactory operates before anything is applied. **[NOT DERIVABLE]** from this family — but their *presentation* is transferable, and `iamlive`'s underlying data (`iann0036/iam-dataset`) is directly reusable (§1.3).

### 1.2 `aws-leastprivilege` / `cfnlp` — the closest structural analogue in existence

**[DOCS, README]** Takes a **static CloudFormation template** and emits an IAM policy for the CloudFormation service role. Static manifest in → policy out, no observed traffic. This is exactly compositionfactory's shape.

Its documented generation logic is a three-tier confidence cascade, and it is the single best template for ours:

> 1. Per-type mappings created by incrementally increasing required permissions
> 2. Permissions retrieved from the CloudFormation Registry
> 3. No data available (**a warning will be shown for missed types**)

Two more design decisions worth copying:
- **`Sid` carries attribution:** `"Sid": "LambdaFunction-Create1"`, `"Sid": "AccessAnalyzer-Create1-reg"` — `{LogicalType}-{Operation}{n}`. `--consolidate-policy` strips Sids and merges statements, i.e. **the tool ships an attributed mode and a compact mode**.
- **Update actions are opt-in** (`--include-update-actions`), because update permissions are the least predictable. *Caution: this does not transfer cleanly — Crossplane reconciles continuously, so read/describe is always required, unlike a one-shot CFN deploy.*

**The sobering number:** its per-type (tier 1, hand-curated) list covers **12 resource types** — CloudWatch Alarm, EC2 Instance/SecurityGroup/Subnet/VPC, IAM Role, Lambda Function/Version, Route53 HostedZone, S3 Bucket, SNS Topic, SQS Queue — against ~1,700 CFN types. **Hand-curation reached roughly 1% coverage and the repo is still flagged ":construction: WORK IN PROGRESS".** Any plan that depends on compositionfactory hand-curating per-resource action lists is a plan to cover 1% of the surface. Derive from data; curate only overrides.

### 1.3 `policy_sentry` — intent-to-actions expansion from a scraped database

**[DOCS]** YAML template in (`read:`/`write:`/`list:`/`tagging:`/`permissions-management:` each holding **resource ARNs**), IAM policy out. It scrapes the AWS Service Authorization Reference (Actions, Resources, and Condition Keys) into a local database and expands access *levels* into concrete actions scoped to the supplied ARNs.

Relevant mechanics:
- **Coarse intent → concrete actions.** The user says "Read access to this ARN"; the tool emits the 5 `ssm:Get*` actions. Same shape as SAM connectors' `Read`/`Write` (§4.2).
- **`Sid` encodes provenance:** `SsmReadParameter` = `{Service}{AccessLevel}{ResourceType}`.
- **An explicit uncertainty bucket in the input schema:** a `wildcard-only:` block for "Actions that do not support resource constraints", plus `skip-resource-constraints:` and `exclude-actions:`. The template *names the places where least privilege is not achievable* rather than hiding them.

**Gap for us:** policy_sentry answers "what actions does this ARN + access level imply for a *consumer*". It does **not** answer "what does a provisioner need to CRUD this resource type". Different question — that is what the CFN registry answers.

### 1.4 `cloudsplaining` — assessment, not generation

**[DOCS]** Scans *existing* IAM policies and emits a risk-prioritised HTML report (Data Exfiltration / Infrastructure Modification / Resource Exposure / Privilege Escalation). Does not generate policies. Its transferable idea is the **exclusions file** (`cloudsplaining create-exclusions-file` → `exclusions.yml`), on the stated rationale: *"Cloudsplaining tool does not attempt to understand the context behind everything in your AWS account… Only you know the context behind the design of your AWS infrastructure."* A generated-permissions feature needs the same escape hatch — a checked-in suppression/override file.

### 1.5 `tfsec` / `checkov` — **[NOT DERIVABLE]**, they only check

**[DOCS]** Both are detection-only: they flag over-permissive IAM and misconfigurations and link to remediation prose. Neither generates a policy. (`tfsec` was deprecated into Trivy over 2023–2024.) No prior art here beyond finding-list presentation.

### 1.6 Is there a Terraform-resource → IAM-actions mapping to reuse? **[NOT DERIVABLE]**

This matters because upjet providers are generated from Terraform providers, so a TF-resource mapping would be a direct hit.

**[VERIFIED]** I enumerated the full `iann0036/iam-dataset` tree (8,574 blobs, untruncated). It contains `aws/map.json` (**SDK method** → IAM action), `aws/iam_definition.json`, `gcp/map.json`, `azure/map.json` — and **no Terraform directory at all**. The AWS map keys on `Budgets.CreateBudget`-style SDK method names, not `aws_sqs_queue`.

**[DOCS]** Searches for a maintained per-resource TF→IAM mapping returned only prose guidance and `airiam` (runtime-derived). HashiCorp does not publish required IAM per resource.

**Conclusion:** there is no off-the-shelf Terraform-resource→IAM mapping. The CFN registry (§3.4 / decision 3) is the closest usable substitute, joined by resource identity rather than by Terraform lineage — with the ~50% naive join rate measured above.

---

## 2. Kubernetes ecosystem

### 2.1 Does anything generate RBAC *statically from manifests*? Essentially no.

| Tool | Input | Generates? | Verdict |
|---|---|---|---|
| `audit2rbac` (liggitt) | Audit log JSON + username | **Yes** — Role + RoleBinding | Observed traffic, not static |
| `rbac-tool auditgen` (alcideio/Rapid7) | Audit events | **Yes** | Observed traffic |
| `rbac-tool gen` | Discovery API + allow/deny flags | **Yes, but** | See below |
| `Audicia` | Audit logs (operator) | **Yes** | Observed traffic |
| `krane` (appvia) | Existing cluster RBAC | No — risk report + dashboard | Static *analysis* of RBAC, not generation |
| `rakkess` | Live cluster | No — access matrix | Query |
| `kubectl-who-can` (Aqua) | Live cluster | No | Query: "which subjects have RBAC permissions to VERB TYPE" |
| `rbac-manager` | Declarative CRD | No — applies what you wrote | Management |

**[VERIFIED, README]** `rbac-tool gen` is **not** manifest-derived. Its own docs: *"`rbac-tool` reads from the Kubernetes discovery API the available API Groups and resources, which represents the 'world' of resources. Based on the command line options, generate an explicit Role/ClusterRole that avoid wildcards by expanding wildcards to the available 'world' resources."* It is subtractive — `--deny-resources=secrets.,services. --allowed-verbs=get,list` — i.e. "everything except". That is the opposite of least privilege from known intent. **[NOT DERIVABLE]** as prior art for us.

**So: the static-from-manifests niche is genuinely empty.** Every generator in the Kubernetes ecosystem derives from observed traffic. We know the GVKs at design time, which none of them do.

### 2.2 The one real static precedent: `controller-gen` RBAC markers **[VERIFIED — I ran it]**

kubebuilder/operator-sdk take static source annotations and emit a ClusterRole. This is the closest thing to our derivation, and I built it and ran it to capture the exact output contract.

Input markers:
```go
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs;cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=*
```
`controller-gen rbac:roleName=compositionfactory-generated paths=./api/... output:dir=./out` produced:
```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: compositionfactory-generated
rules:
- apiGroups: [""]
  resources: [configmaps]
  verbs: [get, list, watch]
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, delete, get, list, patch, update, watch]
- apiGroups: [batch]
  resources: [cronjobs, jobs]
  verbs: [create, delete, get, list, patch, update, watch]
- apiGroups: [networking.k8s.io]
  resources: [ingresses]
  verbs: ['*']
```
Behaviours worth matching or deliberately rejecting:
- **Deterministic ordering** — apiGroups, resources and verbs all alphabetised. Essential for clean git diffs. **Match this.**
- **Merges rules with identical verb sets** (`jobs`+`cronjobs` collapsed). **Reject this** — it is what destroys attribution, and RBAC's additive union means un-merged is free.
- **Passes `*` through unexpanded.** Note it does *not* apply its own wildcard-reduction.
- **Zero attribution.** Nothing in the output says which file, type, or marker produced which rule. This is the concrete gap compositionfactory fills.
- Leading `---`, `roleName` supplied out-of-band by flag.

---

## 3. Crossplane-specific

### 3.1 What Crossplane's RBAC manager generates automatically **[VERIFIED]**

The RBAC manager runs as `crossplane rbac start --provider-clusterrole=crossplane:allowed-provider-permissions`. The core `crossplane` ClusterRole is **aggregated**:
```json
{"clusterRoleSelectors": [{"matchLabels": {"rbac.crossplane.io/aggregate-to-crossplane": "true"}}]}
```
It auto-creates, per XRD:
```yaml
# crossplane:composite:xqueues.platform.hooli.tech:aggregate-to-crossplane
labels: {rbac.crossplane.io/aggregate-to-crossplane: "true"}
rules:
- apiGroups: [platform.hooli.tech]
  resources: [xqueues, xqueues/status]
  verbs: ["*"]
- apiGroups: [platform.hooli.tech]
  resources: [xqueues/finalizers]
  verbs: [update]
```
and per provider, an equivalent block for every MR group — including **both** the cluster-scoped and namespaced upjet variants (`sqs.aws.upbound.io` *and* `sqs.aws.m.upbound.io`).

**This is the verb template to mirror for composed objects: `["*"]` on the resource and `/status`, plus `update` on `/finalizers`.** It is Crossplane's own answer to "what do I need to manage this kind", and it is generated, not guessed.

### 3.2 What it does **not** generate — the actual failure surface **[VERIFIED]**

`crossplane:system:aggregate-to-crossplane` grants a fixed set of native resources: `events`, `customresourcedefinitions`, `secrets`, `serviceaccounts`+`services`, `deployments`, `configmaps`+`leases`, webhook configurations, and `*` on the four Crossplane API groups.

**Critically, that list exists for Crossplane's own internals** (provider Deployments, webhook Services/ServiceAccounts, connection Secrets, leader-election Leases) — it is not a deliberate grant for composed objects. It just happens to overlap. SubjectAccessReview against `system:serviceaccount:crossplane-system:crossplane`:

```
configmaps                              create=yes  get=yes  watch=yes
secrets                                 create=yes  get=yes  watch=yes
services                                create=yes  get=yes  watch=yes
serviceaccounts                         create=yes  get=yes  watch=yes
deployments.apps                        create=yes  get=yes  watch=yes
statefulsets.apps                       create=no   get=no   watch=no
jobs.batch                              create=no   get=no   watch=no
cronjobs.batch                          create=no   get=no   watch=no
ingresses.networking.k8s.io             create=no   get=no   watch=no
networkpolicies.networking.k8s.io       create=no   get=no   watch=no
persistentvolumeclaims                  create=no   get=no   watch=no
horizontalpodautoscalers.autoscaling    create=no   get=no   watch=no
roles.rbac.authorization.k8s.io         create=no   get=no   watch=no
rolebindings.rbac.authorization.k8s.io  create=no   get=no   watch=no
poddisruptionbudgets.policy             create=no   get=no   watch=no
namespaces                              create=no   get=no   watch=no
pods                                    create=no   get=no   watch=no
```

**This quantifies the "#1 why-nothing-happens failure" that prior research flagged but never counted: 12 of 17 (71%) of common composable native kinds silently fail.** Two UX consequences: (a) still emit rules for the 5 that pass, because they pass by accident and are not contractual — but *badge them "already satisfied"* so the user is not asked to apply redundant YAML; (b) the 12 are the demo. A canvas with a `Job` node that says "this will not reconcile until you apply this ClusterRole" is the feature.

**[DOCS]** Crossplane docs confirm the manual remedy and give the canonical artifact shape:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cnpg:aggregate-to-crossplane
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
- apiGroups: [postgresql.cnpg.io]
  resources: [clusters]
  verbs: ["*"]
```
with the warning that the label *"is critical. It configures the role to aggregate to Crossplane's primary cluster role"*, and that disabling the RBAC manager means manually granting access to *any* kind including XRs and MRs. Note the doc's own example uses `verbs: ["*"]` — even upstream's canonical example is not least-privilege.

**[DOCS]** The RBAC manager design doc explicitly declines to generalise: it considered a "rule driven" approach and rejected it — *"Crossplane is choosing to be opinionated about its RBAC roles at this time."* **This is the design space compositionfactory is filling, and upstream has stated it will not fill it.**

### 3.3 The two dead ends for shipping the artifact **[VERIFIED]**

- **No `permissionRequests` on Configurations.** I dumped the spec properties of `configurations`, `configurationrevisions` and `providerrevisions.pkg.crossplane.io`; no key containing "permission" exists on any served version. (`crossplane:allowed-provider-permissions` exists as the aggregation *ceiling* for provider packages, and currently resolves to **zero rules** — its selector matches nothing.)
- **A Configuration package cannot carry a ClusterRole.** `crossplane xpkg build` on a package root containing the doc-canonical ClusterRole above:
  ```
  crossplane: error: failed to build package: failed to parse package: .../rbac.yaml position:0:
  no kind "ClusterRole" is registered for version "rbac.authorization.k8s.io/v1" in scheme "pkg/runtime/scheme.go:111"
  ```
  Hard parse failure, not a warning. **The permissions artifact must live outside the package.**

### 3.4 Does anything in the Crossplane ecosystem emit IAM? Only hand-written workload policies. **[VERIFIED]**

`awslabs/crossplane-on-eks` (the flagship AWS blueprint repo, 532 paths) has `compositions/upbound-aws-provider/iam-policy/` containing **10 hand-written policy compositions**: `s3-read`, `s3-write`, `s3-write-firehose`, `sqs-read`, `sqs-write`, `dynamodb-write`, `kms-read`, `lambda-invoke`, `firehose-write`, `cloudwatch-metrics-write`.

Each is a `Composition` on an `IAMPolicy` XRD that string-formats a policy document around a `spec.resourceArn`, e.g. `sqs-write`:
```json
{"Effect":"Allow","Action":["sqs:SendMessage","sqs:SetQueueAttributes"],"Resource":["%s"]}
```
and `sqs-read`: `["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"]`.

Three conclusions:
- **These are *workload* IAM (what the app consuming the queue needs), not *control-plane* IAM (what the provider needs to create the queue).** The distinction is load-bearing and is the single most important framing decision for this feature (§6.1).
- **Nothing is generated.** Ten hand-curated action lists, checked into YAML, labelled `iam.awsblueprints.io/policy-type: read|write`.
- **[NOT DERIVABLE]** Nowhere in the repo — or in Upbound/Crossplane docs — is there a published per-resource IAM policy for the *provider's own credentials*. The Crossplane security blog post covers authentication mechanisms only; asked directly about required permissions per provider, it has none, and itself concedes documentation gaps. **The de facto community answer is `AdministratorAccess`.** That is the over-grant this feature exists to fix, and it is an uncontested gap.

### 3.5 The reusable AWS data source **[VERIFIED]**

Since no Crossplane-native source exists, the CloudFormation Registry is the substitute. Both endpoints are public and unauthenticated:
- `https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip` → 2.97 MB, **1,722 schemas**
- `https://schema.cloudformation.us-east-1.amazonaws.com/aws-sqs-queue.json` → single type, 13.6 KB

Coverage: **1,577 / 1,722 (91.6%)** carry at least one handler permission list; **9,033** distinct IAM actions; 145 types have none (older/edge services — `AWS::Pinpoint::App`, `AWS::Glue::Partition`, `AWS::WAFRegional::*`, `AWS::MediaLive::Channel`, `AWS::Config::ConfigurationRecorder`…).

The handler decomposition maps cleanly onto Crossplane's reconcile loop — `create`/`read`/`update`/`delete` are exactly the MR lifecycle, and `read` is needed **continuously**, not once.

Join quality (upjet MR kind → `AWS::{service}::{Kind}`), measured against the installed CRDs:
```
HIT   sqs.aws.m.upbound.io  Queue                   -> aws::sqs::queue
HIT   sqs.aws.m.upbound.io  QueuePolicy             -> aws::sqs::queuepolicy
miss  sqs.aws.m.upbound.io  QueueRedrivePolicy      -> (no CFN equivalent; folded into Queue attributes)
miss  sqs.aws.m.upbound.io  QueueRedriveAllowPolicy -> (same)
```
2/4 on real resources (the other misses in the run were `ProviderConfig`/`ProviderConfigUsage` kinds, correctly excluded as non-cloud). **The failure mode is systematic and predictable: Terraform splits into separate resources what CloudFormation models as attributes.** Those cases are not errors to hide — they are `unknown` entries to surface, and usually the parent type's actions already cover them (`sqs:SetQueueAttributes` covers redrive policy).

**GCP [VERIFIED]:** `iann0036/iam-dataset` `gcp/map.json` (2.65 MB) covers 290 services / 10,464 API methods, of which **6,185 (59.1%)** carry permission data — keyed by API method (`pubsub.projects.schemas.create` → `pubsub.schemas.create`), not by resource type. Materially weaker and differently shaped than AWS. Treat GCP as a v2 concern with lower confidence, and say so in the UI.

---

## 4. Presentation

### 4.1 What form does generated output take?

Across every generator surveyed: **a complete, copy-pasteable, apply-ready document — never a diff, never a fragment.**

- `audit2rbac` **[DOCS]** — writes a multi-document YAML stream (Role **and** RoleBinding together) to stdout, designed for `audit2rbac ... > alice-roles.yaml` then `kubectl create -f`. Every generated object is stamped with identifying labels:
  ```yaml
  labels:
    audit2rbac.liggitt.net/generated: "true"
    audit2rbac.liggitt.net/user: alice
  name: audit2rbac:alice
  ```
  **Two ideas to copy: (a) emit the binding alongside the role so the artifact is complete and actually takes effect; (b) label generated objects so they are identifiable and safely regenerable.** The `audit2rbac:` name prefix does the same job in the name.
- `controller-gen` **[VERIFIED]** — writes `out/role.yaml`, one file, deterministic order, meant to be committed and diffed.
- `cfnlp` / `policy_sentry` **[DOCS]** — JSON policy document to stdout.
- `cloudsplaining` **[DOCS]** — the outlier: an HTML report *plus* a raw JSON data file. But it is an assessment tool; its report is findings, not an artifact to apply.

**Implication:** the GUI's job is not to invent a format. It is to show the artifact and let the user copy or save it. The novel surface is the *attribution overlay*, not the document.

### 4.2 Per-resource attribution — three real mechanisms

1. **`Sid` as the attribution channel (IAM).** Independently arrived at by two tools:
   - `cfnlp`: `"Sid": "LambdaFunction-Create1"`, `"AccessAnalyzer-Create1-reg"` — `{Type}-{Op}{n}`, plus `-reg` marking rows that came from the registry rather than a curated mapping. **That suffix is provenance encoded in the artifact itself.**
   - `policy_sentry`: `"Sid": "SsmReadParameter"`, `"SecretsmanagerTaggingSecret"` — `{Service}{AccessLevel}{ResourceType}`.
   - Both offer a consolidated mode that strips Sids (`--consolidate-policy`), i.e. **attributed-by-default with an opt-out for compactness**.
   - **Constraint to design around: IAM `Sid` accepts alphanumerics only** — no hyphens, dots or underscores. Canvas node names must be sanitised (`sqs-queue` → `SqsQueue`), so keep a node-id ↔ Sid map rather than assuming round-tripping.
2. **Labels/annotations and name prefixes (Kubernetes).** RBAC rules have no per-rule metadata field, so attribution must go either in object-level labels (`audit2rbac`'s approach) or in YAML comments. Comments survive the file and the git diff but are stripped by `kubectl apply` — acceptable, since the reviewer is the audience, not the API server.
3. **Edge-scoped permissions (AWS SAM connectors / Infrastructure Composer)** — see below.

### 4.3 AWS Infrastructure Composer + SAM connectors — the closest UX analogue that exists

**[DOCS]** Infrastructure Composer is a visual canvas where you drag resources and draw connections, and *"as you use the editor to connect resources together, [it] is designed to translate the intention to integrate two services into the corresponding IaC configuration for relevant service integrations and IAM permissions that you can inspect or modify at any time."* Dragging an S3 bucket and connecting it to a Lambda yields the IAM policy, the event subscription and scaffolded files.

The underlying mechanism is `AWS::Serverless::Connector`, and its design is worth studying closely:

- **Permissions attach to *edges*, not nodes.** You declare `Source` + `Destination` + `Permissions: [Read, Write]`; SAM expands to concrete actions. `AWS::Lambda::Function` → `AWS::SQS::Queue` `Read` yields `[sqs:ReceiveMessage, sqs:GetQueueAttributes]`; `Write` yields `[sqs:DeleteMessage, sqs:SendMessage, sqs:ChangeMessageVisibility, sqs:PurgeQueue]`. **For a node-graph GUI this is the key structural insight: workload permissions are a property of the connection between two nodes, and a canvas is the natural authoring surface for exactly that.**
- **Coarse intent → concrete actions**, same as policy_sentry's access levels. Users pick `Read`/`Write`, never individual actions.
- **ARN templating with explicit placeholders:** `%{Destination.Arn}`, `%{Destination.Arn}/*`, `%{Source.Arn}`.
- **The policy's *attachment target* varies and is documented per pair** — sometimes an identity policy on the source's role (Lambda→S3), sometimes a **resource policy on the destination** (`AWS::SQS::QueuePolicy` for Events→SQS, `AWS::SNS::TopicPolicy`, `AWS::Lambda::Permission`). Directly relevant: compositionfactory composes MRs, so the resource-policy case should be emitted as a *composed `sqs.aws.upbound.io/QueuePolicy` node on the canvas*, not as a side-car document.
- **Coverage is hand-curated and finite** — roughly 78 source→destination pairs over ~15 service types, with an explicit escape hatch: *"To request new connections, submit a new issue."* Even AWS, on its own services, curates by hand and grows by user request. **Same lesson as cfnlp's 12 types: curate the edges, derive the nodes.**

### 4.4 What makes a generated policy trustworthy to a reviewer

Synthesising across the tools, five properties:

1. **Deterministic output** — stable ordering so re-running produces an empty diff (`controller-gen` sorts everything). Without this, review is impossible.
2. **Attribution** — every statement traceable to a cause (`Sid`, labels).
3. **Provenance per entry** — *how* was this derived (§5).
4. **Explicit holes** — placeholders and named gaps rather than silent omission or silent wildcards.
5. **An override/suppression file** — `cloudsplaining`'s `exclusions.yml`, `policy_sentry`'s `exclude-actions`/`skip-resource-constraints`. The generator must not be the final authority.

---

## 5. The trust problem

### 5.1 The two failure modes, and which one tools optimise against

- **Under-grant** → provisioning silently stalls. In our case this is the dominant risk: **[VERIFIED]** 12 of 17 common composed native kinds are denied today, and Crossplane surfaces this as an XR that simply never becomes ready.
- **Over-grant** → invisible until audit. Every tool surveyed biases *toward* over-granting to avoid the visible annoyance: `iamlive --force-wildcard-resource`, `rbac-tool gen`'s "everything except", and Crossplane's own doc example using `verbs: ["*"]`.

**Design consequence:** we should not follow that bias blindly. For the k8s half we can be exact *and* safe because we can verify (§5.3). For the cloud half, note that Crossplane's continuous reconciliation means a missing `read`/`Describe` action produces a permanently-degraded resource rather than a clean failure — so read actions must never be trimmed.

### 5.2 How existing tools communicate confidence

**IAM Access Analyzer is the reference implementation. [DOCS]** Its whole UX is built around admitting what it does not know:

- **Two explicitly named output tiers:** *"Policy with action-level information"* (for supported services, actions are listed) versus *"Policy with service-level information"* (derived from last-accessed data, actions unknown). There is a published list of which services support action level.
- **The UI physically separates them:** an "Actions included in the generated policy" list, and a distinct **"Services used"** section where *"Information about which actions were used might not be available for the services listed in this section. Use the menus for each service listed to manually choose the actions that you want to include in the policy."* — the unknown tier is rendered as **an interactive to-do list, not an omission**.
- **Visible placeholders:** *"The policy template contains resource ARN placeholders for actions that support resource-level permissions… You can replace the placeholder resource ARNs with valid resource ARNs for your use case."*
- **Published blind spots**, plainly stated: data events are not captured; *"The `iam:PassRole` action is not tracked by CloudTrail and is not included in generated policies."*
- **Scope disclaimer:** *"Do not use policy generation for auditing purposes; use CloudTrail instead."*
- **Deliberate staleness:** generated policies expire from the console after 7 days, so nobody applies a stale artifact.

**`iann0036/iam-dataset` — per-entry provenance in the data model. [VERIFIED]** The GCP map tags every permission with `discoveryMethodologies`:

| methodology | count | reading |
|---|---|---|
| `manual` | 3,849 | human-curated |
| `restcrawliamblockv1` | 2,605 | crawled from IAM block metadata |
| `restcrawlv1` | 1,828 | crawled from REST docs |
| `fuzzv1` | 18 | discovered by fuzzing |

Combinations matter: 778 entries carry both `manual` and `restcrawliamblockv1` — **independent corroboration is itself a confidence signal.** The AWS map has the analogous `"undocumented": true` flag, defined as *"marks that the action is not documented within the AWS IAM documentation (SAR) — typically these are discovered through error messages."*

**`cfnlp` [DOCS]** degrades explicitly rather than silently: tier-1 curated mappings are *"as specific as possible… Wildcard actions are never used"*, tier-2 registry-derived output is coarser — *"All resources will be wildcarded and no conditions will apply"* — and tier 3 prints a warning naming the missed types.

**`audit2rbac` [DOCS]** communicates its central caveat as a *procedure*, not a disclaimer: *"To exercise all API calls, it is sometimes necessary to grant broad access to a user or application to avoid short-circuiting code paths on failed API requests."* i.e. its output is only as complete as the traffic it saw. And it tells the user to *"Inspect the output to verify the generated roles/bindings"* before applying — a review step baked into the documented workflow.

### 5.3 What nobody has, and we do

Every tool above is guessing about a system it cannot interrogate. **[VERIFIED]** We can ask the cluster directly:

```
kubectl auth can-i create jobs.batch --as=system:serviceaccount:crossplane-system:crossplane -n default
```

This is a plain `SubjectAccessReview` — read-only, fast, no mutation, works over any kubeconfig. It converts the entire Kubernetes half of the feature from *inferred* to *verified*, and additionally yields the "already satisfied" state that stops us nagging users about the 5 kinds that already work. **No prior-art tool in this survey can validate its own output against the target system.** Lead with it.

---

## 6. Recommendation for compositionfactory

### 6.1 Frame the problem on two axes first — this is the decision everything else depends on

|  | **Control-plane** (what the provisioner needs) | **Workload** (what the running app needs) |
|---|---|---|
| **Kubernetes** | ClusterRole for the Crossplane SA to create composed native objects | Roles/SAs for the composed app |
| **Cloud** | IAM for the provider's credentials: `sqs:CreateQueue`… | IAM for the consumer: `sqs:SendMessage`… |

- The user's ask ("RBAC needed for objects dragged to canvas") is the **top-left** cell, and it is the one with a hard, verifiable, currently-broken answer. **Ship it first.**
- **Top-right** is the second artifact: derivable at 91.6% from the CFN registry, and the gap nobody has filled (§3.4).
- **Both bottom cells are composition content, not side artifacts.** A workload IAM policy should appear on the canvas as an `iam.aws.upbound.io/Policy` or `sqs.aws.upbound.io/QueuePolicy` **node**, authored SAM-connector-style by drawing an edge (§4.3) — exactly what `crossplane-on-eks` hand-writes today. Do not emit these as files; that would put permissions in two places and desynchronise them.

### 6.2 Two artifacts, both outside the package

Forced by §3.3 — the package parser rejects a ClusterRole outright.

```
<composition-dir>/
  composition.yaml
  definition.yaml
  permissions/
    rbac.yaml              # ClusterRole (+ nothing else needed — aggregation replaces binding)
    iam-controlplane.json  # provider credential policy
    permissions.lock.json  # provenance sidecar: per-entry tier + source + node ids
    overrides.yaml         # user suppressions/additions, hand-edited, never regenerated
```

- `rbac.yaml` is a **single aggregating ClusterRole**, named `<xrd-name>:aggregate-to-crossplane`, carrying `rbac.crossplane.io/aggregate-to-crossplane: "true"`. No RoleBinding is needed — aggregation is the binding mechanism, which is why this differs from `audit2rbac`'s Role+RoleBinding pair.
- Stamp generated objects with identifying labels, per `audit2rbac`: `compositionfactory.io/generated: "true"`, `compositionfactory.io/source-composition: <name>`.
- `permissions.lock.json` keeps machine-readable provenance out of the apply-able artifacts, so `rbac.yaml` stays clean YAML a reviewer can read and `kubectl apply -f`.
- `overrides.yaml` is non-negotiable (§4.4 property 5).

### 6.3 Attribution per node

- **IAM:** one `Sid` per node per lifecycle phase, `cfnlp`-style: `SqsQueueCreate1`. Alphanumeric-only (§4.2). Offer a "consolidate" toggle that merges and drops Sids, matching both prior-art tools.
- **RBAC:** **do not merge rules across nodes.** RBAC is an additive union with no deny, so N un-merged rules are semantically identical to the merged form — attribution is free. Precede each rule with a YAML comment naming the contributing node(s), and alphabetise apiGroups/resources/verbs within each rule so diffs stay clean (`controller-gen`'s one genuinely good habit).
  ```yaml
  # node: worker-job (batch/v1 Job)
  - apiGroups: [batch]
    resources: [jobs, jobs/status]
    verbs: ["*"]
  ```
- Verb template comes from Crossplane's own generated roles (§3.1): `["*"]` on resource + `/status`, `update` on `/finalizers`. Do not invent a narrower set without testing it — an under-granted composed object fails silently.
- **GUI:** the artifact panel and the canvas share one selection model. Selecting a node highlights its statements/rules; selecting a statement highlights the contributing node(s). The headline the user asked for — "these 4 nodes require these 11 actions" — is the panel header, with each line badged by tier.

### 6.4 Surfacing uncertainty — three tiers, and never omit

Modelled on Access Analyzer's two tiers plus `cfnlp`'s third, with per-entry provenance from `iam-dataset`:

| Tier | Meaning | Source | UI |
|---|---|---|---|
| **verified** | Confirmed against the target, or AWS-authored | `SubjectAccessReview` says yes/no; CFN handler permissions | Plain |
| **inferred** | Derived by heuristic | kind→CFN typeName join; RBAC verb template | Badged, tooltip naming the rule applied |
| **unknown** | No mapping found | 145 CFN types w/o handlers; TF-only kinds like `QueueRedrivePolicy` | **Rendered as an actionable to-do list, never dropped** |

- Split the k8s "verified" tier into **already-satisfied** vs **needs-granting**, from the live SubjectAccessReview. This is the differentiator (§5.3). Degrade gracefully to `inferred` when no cluster is reachable — and say so rather than pretending.
- Publish blind spots the way AWS does. Known ones today: cross-resource actions like `iam:PassRole` are not derivable from resource schemas; GCP is only 59.1% covered and keyed by API method not resource type; Terraform-granularity kinds with no CFN equivalent.
- **Never emit a silent wildcard.** If the derivation cannot narrow a statement, it is an `unknown` entry with a visible reason — that is the whole difference between this feature and `AdministratorAccess`.

### 6.5 Sequencing

1. **K8s control-plane RBAC + SubjectAccessReview verification.** Highest confidence, verifiable, fixes a quantified 71% silent-failure rate, and no prior art competes.
2. **AWS control-plane IAM from the CFN registry**, with an `unknown` tier and a curated override table for the Terraform-granularity misses. Uncontested gap (§3.4).
3. **Workload permissions as canvas edges** producing composed `Policy`/`QueuePolicy` MR nodes, SAM-connector style. Curated per pair; grows by request, as AWS's own does.
4. **GCP**, explicitly lower-confidence.

---

## Negative results (a negative result is a useful result)

- **[NOT DERIVABLE]** No tool generates Kubernetes RBAC statically from workload manifests. Every generator (`audit2rbac`, `rbac-tool auditgen`, Audicia) derives from audit logs; `rbac-tool gen` is wildcard-expansion-with-denylist, not intent-derived; `krane`/`rakkess`/`kubectl-who-can` only analyse or query what already exists. The niche is empty.
- **[NOT DERIVABLE]** No Terraform-resource → IAM-actions mapping exists to reuse. `iann0036/iam-dataset` (8,574 blobs, verified full tree) has AWS/Azure/GCP maps keyed by **SDK/API method**, and no Terraform directory. HashiCorp publishes no per-resource IAM requirements.
- **[NOT DERIVABLE]** Neither Crossplane nor Upbound publishes the IAM a provider's credentials need, per resource or in aggregate. The security blog post covers authentication only. `crossplane-on-eks` publishes 10 hand-written **workload** policies and zero control-plane policies.
- **[VERIFIED]** A Configuration package cannot carry a ClusterRole (`xpkg build` parse failure), and no `permissionRequests` field exists on any Configuration/ConfigurationRevision/ProviderRevision version in v2.4.0. There is no in-package delivery path.
- **[DOCS]** `tfsec`/`checkov` do not generate policies at all — detection only. (`tfsec` is deprecated into Trivy.)
- **[DOCS]** Hand-curation does not scale: `cfnlp` reached 12 of ~1,700 CFN types; SAM connectors cover ~78 pairs over ~15 service types and grow by GitHub issue. Derive from data; curate only overrides and edges.

---

## Sources

**Ran/parsed directly [VERIFIED]:** live `kind-platform` cluster (`kubectl auth can-i`, ClusterRole and CRD dumps, `crossplane xpkg build`); `controller-gen` v0.16.5; `https://schema.cloudformation.us-east-1.amazonaws.com/CloudformationSchema.zip` and `/aws-sqs-queue.json`; `iann0036/iam-dataset` git tree + `gcp/map.json`; `awslabs/crossplane-on-eks` git tree + `compositions/upbound-aws-provider/iam-policy/*.yaml`; READMEs of `iamlive`, `policy_sentry`, `cloudsplaining`, `iamzero`, `audit2rbac`, `rbac-tool`, `aws-leastprivilege`, `rakkess`, `krane`, `kubectl-who-can`.

**Read [DOCS]:**
- [audit2rbac](https://github.com/liggitt/audit2rbac) · [rbac-tool](https://github.com/alcideio/rbac-tool) · [kubectl-who-can](https://github.com/aquasecurity/kubectl-who-can) · [krane](https://github.com/appvia/krane) · [rakkess](https://github.com/corneliusweig/rakkess) · [rbac.dev](https://rbac.dev/)
- [iamlive](https://github.com/iann0036/iamlive) · [aws-leastprivilege/cfnlp](https://github.com/iann0036/aws-leastprivilege) · [iam-dataset](https://github.com/iann0036/iam-dataset) · [policy_sentry](https://github.com/salesforce/policy_sentry) · [cloudsplaining](https://github.com/salesforce/cloudsplaining) · [iamzero](https://github.com/common-fate/iamzero)
- [IAM Access Analyzer policy generation](https://docs.aws.amazon.com/IAM/latest/UserGuide/access-analyzer-policy-generation.html) · [SAM connector reference](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/reference-sam-connector.html) · [Infrastructure Composer FAQs](https://aws.amazon.com/infrastructure-composer/faqs/)
- [Crossplane Compositions docs](https://docs.crossplane.io/latest/composition/compositions/) · [RBAC manager design doc](https://github.com/crossplane/crossplane/blob/main/design/design-doc-rbac-manager.md) · [Enhancing Security Practices with Crossplane Providers](https://blog.crossplane.io/enhancing-security-practices-with-crossplane-providers/) · [crossplane-on-eks](https://github.com/awslabs/crossplane-on-eks)
