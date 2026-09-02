package catalogue

import (
	"sort"
	"strings"
)

// packageKinds maps known crossplane-contrib provider packages to the primary
// CRD kinds, resources, and service aliases they provide.
var packageKinds = map[string][]string{
	// AWS services
	"provider-aws-s3": {
		"Bucket", "BucketPolicy", "BucketObject", "BucketServerSideEncryptionConfiguration",
		"BucketVersioning", "BucketLifecycleConfiguration", "BucketCorsConfiguration",
		"BucketAcl", "BucketPublicAccessBlock", "BucketWebsiteConfiguration", "BucketNotification",
		"S3", "S3Bucket",
	},
	"provider-aws-rds": {
		"Instance", "DBInstance", "DatabaseInstance", "RDSInstance", "Cluster", "DBCluster",
		"DBSubnetGroup", "ParameterGroup", "DBParameterGroup", "OptionGroup",
		"ClusterInstance", "ClusterParameterGroup", "ClusterSnapshot", "RDS", "Database",
	},
	"provider-aws-sqs": {
		"Queue", "QueuePolicy", "QueueRedrivePolicy", "QueueRedriveAllowPolicy", "SQS", "SQSQueue",
	},
	"provider-aws-sns": {
		"Topic", "TopicPolicy", "TopicSubscription", "SNS", "SNSTopic",
	},
	"provider-aws-dynamodb": {
		"Table", "TableItem", "GlobalTable", "DynamoDB", "DynamoDBTable",
	},
	"provider-aws-iam": {
		"Role", "Policy", "User", "AccessKey", "Group", "InstanceProfile",
		"OpenIDConnectProvider", "RolePolicyAttachment", "UserPolicyAttachment",
		"GroupPolicyAttachment", "ServerCertificate", "ServiceSpecificCredential",
		"IAM", "IAMRole", "IAMPolicy", "IAMUser",
	},
	"provider-aws-ec2": {
		"VPC", "Subnet", "SecurityGroup", "SecurityGroupRule", "RouteTable", "Route",
		"InternetGateway", "NATGateway", "EIP", "Instance", "KeyPair", "NetworkInterface",
		"VPCEndpoint", "VPCPeeringConnection", "TransitGateway", "FlowLog", "EC2",
	},
	"provider-aws-eks": {
		"Cluster", "NodeGroup", "FargateProfile", "Addon", "IdentityProviderConfig",
		"AccessEntry", "AccessPolicyAssociation", "EKS", "EKSCluster",
	},
	"provider-aws-kms": {
		"Key", "Alias", "KeyPolicy", "Grant", "Ciphertext", "KMS", "KMSKey",
	},
	"provider-aws-lambda": {
		"Function", "FunctionUrl", "LayerVersion", "Permission", "EventSourceMapping", "Alias", "Lambda",
	},
	"provider-aws-apigatewayv2": {
		"API", "Route", "Stage", "Integration", "Authorizer", "Deployment", "DomainName", "VPCLink", "APIGateway",
	},
	"provider-aws-acm": {
		"Certificate", "CertificateValidation", "ACM", "ACMCertificate",
	},
	"provider-aws-route53": {
		"Record", "Zone", "HealthCheck", "DelegationSet", "Route53", "DNSZone", "DNSRecord",
	},
	"provider-aws-elasticache": {
		"Cluster", "ReplicationGroup", "SubnetGroup", "ParameterGroup", "User", "ServerlessCache", "ElastiCache", "Redis",
	},
	"provider-aws-secretsmanager": {
		"Secret", "SecretVersion", "SecretPolicy", "SecretsManager",
	},
	"provider-aws-cloudwatch": {
		"MetricAlarm", "CompositeAlarm", "Dashboard", "LogGroup", "MetricStream", "CloudWatch",
	},
	"provider-aws-cloudwatchlogs": {
		"Group", "Stream", "LogGroup", "LogStream", "MetricFilter", "SubscriptionFilter", "CloudWatchLogs",
	},
	"provider-aws-efs": {
		"FileSystem", "MountTarget", "AccessPoint", "BackupPolicy", "EFS",
	},
	"provider-aws-eventbridge": {
		"Rule", "Bus", "Target", "Archive", "Connection", "EventBridge",
	},
	"provider-aws-cloudtrail": {
		"Trail", "EventDataStore", "CloudTrail",
	},
	"provider-aws-cognitoidentityprovider": {
		"UserPool", "UserPoolClient", "UserPoolDomain", "ResourceServer", "UserGroup", "Cognito",
	},
	"provider-aws-ecs": {
		"Cluster", "TaskDefinition", "Service", "CapacityProvider", "ECS",
	},
	"provider-aws-elasticloadbalancingv2": {
		"LB", "LoadBalancer", "TargetGroup", "Listener", "ListenerRule", "ALB", "NLB", "ELB",
	},
	"provider-aws-elbv2": {
		"LB", "LoadBalancer", "TargetGroup", "Listener", "ListenerRule", "ALB", "NLB", "ELB",
	},
	"provider-aws-cloudfront": {
		"Distribution", "OriginAccessIdentity", "CachePolicy", "ResponseHeadersPolicy", "CloudFront",
	},
	"provider-aws-kafka": {
		"Cluster", "Configuration", "ServerlessCluster", "MSK", "Kafka",
	},

	// GCP services
	"provider-gcp-sql": {
		"DatabaseInstance", "CloudSQL", "CloudSQLInstance", "Instance", "Database", "User", "SSLSubnet", "SSLKey", "BackupRun", "SQL",
	},
	"provider-gcp-storage": {
		"Bucket", "BucketAccessControl", "BucketIAMMember", "BucketIAMBinding", "BucketObject", "DefaultObjectAccessControl", "HmacKey", "GCS", "GCSBucket",
	},
	"provider-gcp-pubsub": {
		"Topic", "Subscription", "TopicIAMMember", "TopicIAMBinding", "SubscriptionIAMMember", "PubSub", "PubSubTopic", "PubSubSubscription",
	},
	"provider-gcp-iam": {
		"ServiceAccount", "ServiceAccountKey", "ServiceAccountIAMMember", "WorkloadIdentityPool", "WorkloadIdentityPoolProvider", "CustomRole", "AccessKey", "IAM",
	},
	"provider-gcp-compute": {
		"Instance", "InstanceGroup", "InstanceTemplate", "Network", "Subnetwork", "Firewall", "Router", "Route", "Disk", "Address", "GlobalAddress", "ForwardingRule", "BackendService", "HealthCheck", "Compute", "GCE",
	},
	"provider-gcp-container": {
		"Cluster", "NodePool", "GKECluster", "GKE", "Container",
	},
	"provider-gcp-bigquery": {
		"Dataset", "Table", "DatasetAccess", "Job", "BigQuery",
	},
	"provider-gcp-cloudfunctions": {
		"Function", "FunctionIAMMember", "CloudFunctions",
	},
	"provider-gcp-cloudrun": {
		"Service", "DomainMapping", "ServiceIAMMember", "CloudRun",
	},
	"provider-gcp-kms": {
		"KeyRing", "CryptoKey", "CryptoKeyIAMMember", "SecretCiphertext", "KMS",
	},
	"provider-gcp-secretmanager": {
		"Secret", "SecretVersion", "SecretIAMMember", "SecretManager",
	},
	"provider-gcp-dns": {
		"ManagedZone", "RecordSet", "Policy", "DNS", "Record",
	},
	"provider-gcp-redis": {
		"Instance", "Redis", "Memorystore",
	},
	"provider-gcp-artifact": {
		"Repository", "RepositoryIAMMember", "ArtifactRegistry",
	},

	// Azure services
	"provider-azure-storage": {
		"Account", "StorageAccount", "Container", "Blob", "Share", "Queue", "Table", "Storage", "AzureStorage",
	},
	"provider-azure-compute": {
		"LinuxVirtualMachine", "WindowsVirtualMachine", "VirtualMachine", "Disk", "Snapshot", "AvailabilitySet", "VirtualMachineScaleSet", "VM",
	},
	"provider-azure-network": {
		"VirtualNetwork", "Subnet", "SecurityGroup", "NetworkSecurityGroup", "PublicIP", "NetworkInterface", "RouteTable", "NATGateway", "VNet", "NSG",
	},
	"provider-azure-containerservice": {
		"KubernetesCluster", "KubernetesClusterNodePool", "AKSCluster", "AKS",
	},
	"provider-azure-cosmosdb": {
		"Account", "SQLDatabase", "SQLContainer", "MongoDatabase", "Table", "CosmosDB",
	},
	"provider-azure-keyvault": {
		"Vault", "Key", "Secret", "Certificate", "AccessPolicy", "KeyVault",
	},
	"provider-azure-dbforpostgresql": {
		"Server", "Database", "FlexibleServer", "Configuration", "FirewallRule", "PostgreSQL", "Postgres",
	},
	"provider-azure-dbformysql": {
		"Server", "Database", "FlexibleServer", "Configuration", "FirewallRule", "MySQL",
	},
	"provider-azure-sql": {
		"Server", "Database", "MSSQLServer", "MSSQLDatabase", "SQLServer", "AzureSQL",
	},
	"provider-azure-servicebus": {
		"Namespace", "Topic", "Queue", "Subscription", "Rule", "ServiceBus",
	},
	"provider-azure-eventhub": {
		"EventHubNamespace", "EventHub", "ConsumerGroup", "AuthorizationRule", "EventHubs",
	},
	"provider-azure-authorization": {
		"RoleAssignment", "RoleDefinition", "UserAssignedIdentity",
	},

	// Native Kubernetes & other ecosystem packages
	"provider-kubernetes": {
		"Object", "ObservedObjectCollection", "Deployment", "Service", "ConfigMap", "Secret",
		"Namespace", "ServiceAccount", "Ingress", "StatefulSet", "DaemonSet", "Job", "CronJob",
		"PersistentVolumeClaim", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding",
	},
	"provider-helm": {
		"Release", "Repository", "HelmRelease",
	},
	"provider-vault": {
		"Secret", "Policy", "AuthBackend", "DatabaseSecretBackendRole", "PKISecretBackend", "VaultSecret",
	},
	"provider-kafka": {
		"Topic", "ACL", "User", "Quota", "KafkaTopic",
	},
	"provider-terraform": {
		"Workspace",
	},
	"provider-ansible": {
		"AnsibleRun", "Playbook",
	},
	"provider-cert-manager": {
		"Certificate", "Issuer", "ClusterIssuer", "CertManager",
	},
}

var (
	// kindToPackages maps lowercase kind/alias names to the list of packages serving them.
	kindToPackages map[string][]string
)

func init() {
	kindToPackages = make(map[string][]string)
	for pkg, kinds := range packageKinds {
		for _, k := range kinds {
			norm := strings.ToLower(k)
			kindToPackages[norm] = append(kindToPackages[norm], pkg)
		}
	}
	// Sort package lists for deterministic lookups
	for k := range kindToPackages {
		sort.Strings(kindToPackages[k])
	}
}

// Kinds returns the list of known kinds and service aliases served by pkgName.
func Kinds(pkgName string) []string {
	if kinds, ok := packageKinds[pkgName]; ok {
		out := make([]string, len(kinds))
		copy(out, kinds)
		return out
	}
	return nil
}

// PackagesForKind returns the package names that serve the given kind or alias name.
func PackagesForKind(kindName string) []string {
	norm := strings.ToLower(kindName)
	if pkgs, ok := kindToPackages[norm]; ok {
		out := make([]string, len(pkgs))
		copy(out, pkgs)
		return out
	}
	return nil
}

// Matches reports whether p matches the search query q.
// It checks name, description, and the indexed kinds and aliases served by p.
func Matches(p Provider, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(p.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Description), q) {
		return true
	}
	for _, k := range packageKinds[p.Name] {
		if strings.Contains(strings.ToLower(k), q) {
			return true
		}
	}
	// Check if the query is a kind that maps to this package
	for _, pkg := range kindToPackages[q] {
		if pkg == p.Name {
			return true
		}
	}
	return false
}

// Search filters entries according to query and type ("provider", "function", or "").
func Search(entries []Provider, query, typ string) []Provider {
	query = strings.ToLower(strings.TrimSpace(query))
	typ = strings.ToLower(strings.TrimSpace(typ))

	out := make([]Provider, 0, len(entries))
	for _, e := range entries {
		isFn := strings.HasPrefix(e.Name, "function-")
		if typ == "function" && !isFn {
			continue
		}
		if typ == "provider" && isFn {
			continue
		}
		if query == "" || Matches(e, query) {
			out = append(out, e)
		}
	}
	return out
}
