// Package cluster implements a lightweight, zero-external-dependency Kubernetes
// client for discovering CustomResourceDefinitions from any live cluster
// (kind, k3s, minikube, EKS, GKE, in-cluster).
package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/koorikla/compositionfactory/internal/schema"
)

// ProviderLabel is the synthetic provider ref assigned to live-cluster CRDs in
// the index and store.
const ProviderLabel = "cluster"

// ClusterInfo describes the cluster connection state and discovery summary.
type ClusterInfo struct {
	Connected bool   `json:"connected"`
	Context   string `json:"context,omitempty"`
	Server    string `json:"server,omitempty"`
	CRDCount  int    `json:"crdCount"`
	Error     string `json:"error,omitempty"`
}

// Client connects to a Kubernetes API server to fetch CRDs.
type Client struct {
	server  string
	context string
	token   string
	client  *http.Client
}

// kubeConfig mirrors the minimal subset of a ~/.kube/config YAML file.
type kubeConfig struct {
	CurrentContext string `json:"current-context"`
	Clusters       []struct {
		Name    string `json:"name"`
		Cluster struct {
			Server                   string `json:"server"`
			CertificateAuthorityData string `json:"certificate-authority-data"`
			CertificateAuthority     string `json:"certificate-authority"`
			InsecureSkipTLSVerify    bool   `json:"insecure-skip-tls-verify"`
		} `json:"cluster"`
	} `json:"clusters"`
	Users []struct {
		Name string `json:"name"`
		User struct {
			Token                 string `json:"token"`
			ClientCertificateData string `json:"client-certificate-data"`
			ClientCertificate     string `json:"client-certificate"`
			ClientKeyData         string `json:"client-key-data"`
			ClientKey             string `json:"client-key"`
		} `json:"user"`
	} `json:"users"`
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster string `json:"cluster"`
			User    string `json:"user"`
		} `json:"context"`
	} `json:"contexts"`
}

// DefaultKubeconfigPath returns standard ~/.kube/config path.
func DefaultKubeconfigPath() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		parts := filepath.SplitList(env)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}

// NewClient creates a Client from kubeconfig bytes or resolves from default locations.
func NewClient(kubeconfigPath, contextName string) (*Client, error) {
	// 1. Try specified or default kubeconfig file
	path := kubeconfigPath
	if path == "" {
		path = DefaultKubeconfigPath()
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			return FromKubeconfig(data, contextName)
		}
		if kubeconfigPath != "" {
			return nil, fmt.Errorf("read kubeconfig %q: %w", kubeconfigPath, err)
		}
	}

	// 2. Try in-cluster service account credentials
	if inClusterClient, err := InClusterClient(); err == nil {
		return inClusterClient, nil
	}

	return nil, fmt.Errorf("no kubernetes configuration found (checked %q and in-cluster)", path)
}

// InClusterClient creates a client using in-cluster pod service account token & CA.
func InClusterClient() (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in-cluster (KUBERNETES_SERVICE_HOST not set)")
	}

	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caFile := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read in-cluster token: %w", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caBytes, err := os.ReadFile(caFile); err == nil {
		caPool := x509.NewCertPool()
		caPool.AppendCertsFromPEM(caBytes)
		tlsConfig.RootCAs = caPool
	}

	serverURL := fmt.Sprintf("https://%s:%s", host, port)
	return &Client{
		server:  serverURL,
		context: "in-cluster",
		token:   strings.TrimSpace(string(tokenBytes)),
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
	}, nil
}

// FromKubeconfig parses raw kubeconfig YAML and builds a Client.
func FromKubeconfig(data []byte, contextName string) (*Client, error) {
	var cfg kubeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	ctxName := contextName
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}
	if ctxName == "" && len(cfg.Contexts) > 0 {
		ctxName = cfg.Contexts[0].Name
	}
	if ctxName == "" {
		return nil, fmt.Errorf("kubeconfig has no contexts")
	}

	var targetCtx *struct {
		Cluster string `json:"cluster"`
		User    string `json:"user"`
	}
	for _, c := range cfg.Contexts {
		if c.Name == ctxName {
			targetCtx = &c.Context
			break
		}
	}
	if targetCtx == nil {
		return nil, fmt.Errorf("context %q not found in kubeconfig", ctxName)
	}

	var clusterURL string
	var caData []byte
	var insecureSkip bool
	for _, cl := range cfg.Clusters {
		if cl.Name == targetCtx.Cluster {
			clusterURL = cl.Cluster.Server
			insecureSkip = cl.Cluster.InsecureSkipTLSVerify
			if cl.Cluster.CertificateAuthorityData != "" {
				caData, _ = base64.StdEncoding.DecodeString(cl.Cluster.CertificateAuthorityData)
			} else if cl.Cluster.CertificateAuthority != "" {
				caData, _ = os.ReadFile(cl.Cluster.CertificateAuthority)
			}
			break
		}
	}
	if clusterURL == "" {
		return nil, fmt.Errorf("cluster %q not found for context %q", targetCtx.Cluster, ctxName)
	}

	var token string
	var clientCertData, clientKeyData []byte
	for _, u := range cfg.Users {
		if u.Name == targetCtx.User {
			token = u.User.Token
			if u.User.ClientCertificateData != "" {
				clientCertData, _ = base64.StdEncoding.DecodeString(u.User.ClientCertificateData)
			} else if u.User.ClientCertificate != "" {
				clientCertData, _ = os.ReadFile(u.User.ClientCertificate)
			}
			if u.User.ClientKeyData != "" {
				clientKeyData, _ = base64.StdEncoding.DecodeString(u.User.ClientKeyData)
			} else if u.User.ClientKey != "" {
				clientKeyData, _ = os.ReadFile(u.User.ClientKey)
			}
			break
		}
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecureSkip,
	}
	if len(caData) > 0 {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caData)
		tlsConfig.RootCAs = pool
	}
	if len(clientCertData) > 0 && len(clientKeyData) > 0 {
		cert, err := tls.X509KeyPair(clientCertData, clientKeyData)
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}

	return &Client{
		server:  strings.TrimRight(clusterURL, "/"),
		context: ctxName,
		token:   token,
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
	}, nil
}

// Server returns the configured API server URL.
func (c *Client) Server() string { return c.server }

// Context returns the active context name.
func (c *Client) Context() string { return c.context }

// crdList represents the JSON response from /apis/apiextensions.k8s.io/v1/customresourcedefinitions
type crdList struct {
	Items []json.RawMessage `json:"items"`
}

// FetchCRDs queries the Kubernetes API server for all CustomResourceDefinitions.
func (c *Client) FetchCRDs(ctx context.Context) ([]schema.CRD, error) {
	reqURL := c.server + "/apis/apiextensions.k8s.io/v1/customresourcedefinitions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create crd request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to cluster at %s: %w", c.server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("cluster returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var list crdList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("decode crd list: %w", err)
	}

	rawDocs := make([][]byte, len(list.Items))
	for i, it := range list.Items {
		rawDocs[i] = []byte(it)
	}

	crds, err := schema.ParseCRDs(rawDocs)
	if err != nil {
		return nil, fmt.Errorf("parse cluster crds: %w", err)
	}

	for i := range crds {
		if !crds[i].IsManaged() {
			hasForProvider := false
			if v, err := crds[i].Preferred(); err == nil {
				if spec, ok := v.Properties["spec"].(map[string]any); ok {
					if props, ok := spec["properties"].(map[string]any); ok {
						if _, ok := props["forProvider"]; ok {
							hasForProvider = true
						}
					}
				}
			}
			if !hasForProvider {
				crds[i].Native = true
			}
		}
	}

	return crds, nil
}

// Info returns the current cluster connection status and metadata.
func (c *Client) Info(ctx context.Context) ClusterInfo {
	info := ClusterInfo{
		Context: c.context,
		Server:  c.server,
	}
	crds, err := c.FetchCRDs(ctx)
	if err != nil {
		info.Connected = false
		info.Error = err.Error()
		return info
	}
	info.Connected = true
	info.CRDCount = len(crds)
	return info
}
