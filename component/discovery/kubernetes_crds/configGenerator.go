package kubernetes_crds

// SEE https://github.com/prometheus-operator/prometheus-operator/blob/main/pkg/prometheus/promcfg.go

import (
	"regexp"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/agent/pkg/util/k8sfs"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	commonConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	promk8s "github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/model/relabel"
)

type configGenerator struct {
	config   *Config
	logger   log.Logger
	secretfs *k8sfs.FS
}

// the k8s sd config is mostly dependent on our local config for accessing the kubernetes cluster.
// if undefined it will default to an in-cluster config
func (cg *configGenerator) generateK8SSDConfig(
	namespaceSelector v1.NamespaceSelector,
	namespace string,
	role promk8s.Role,
	attachMetadata *v1.AttachMetadata,
) *promk8s.SDConfig {
	cfg := &promk8s.SDConfig{
		Role: role,
	}
	namespaces := cg.getNamespacesFromNamespaceSelector(namespaceSelector, namespace)
	if len(namespaces) != 0 {
		cfg.NamespaceDiscovery.Names = namespaces
	}
	if cg.config.KubeConfig != "" {
		cfg.KubeConfig = cg.config.KubeConfig
	}
	if cg.config.ApiServerConfig != nil {
		apiCfg := cg.config.ApiServerConfig
		hCfg := apiCfg.HTTPClientConfig
		cfg.APIServer = apiCfg.Host.Convert()

		if hCfg.BasicAuth != nil {
			cfg.HTTPClientConfig.BasicAuth = hCfg.BasicAuth.Convert()
		}

		if hCfg.BearerToken != "" {
			cfg.HTTPClientConfig.BearerToken = commonConfig.Secret(hCfg.BearerToken)
		}
		if hCfg.BearerTokenFile != "" {
			cfg.HTTPClientConfig.BearerTokenFile = hCfg.BearerTokenFile
		}
		cfg.HTTPClientConfig.TLSConfig = *hCfg.TLSConfig.Convert()
		if hCfg.Authorization != nil {
			if hCfg.Authorization.Type == "" {
				hCfg.Authorization.Type = "Bearer"
			}
			cfg.HTTPClientConfig.Authorization = hCfg.Authorization.Convert()
		}
	}
	if attachMetadata != nil {
		cfg.AttachMetadata.Node = attachMetadata.Node
	}
	return cfg
}

func parseRegexp(s string, l log.Logger) relabel.Regexp {
	r, err := relabel.NewRegexp(s)
	if err != nil {
		level.Error(l).Log("msg", "failed to parse regex", "err", err)
	}
	return r
}

func (cg *configGenerator) generateSafeTLS(namespace string, tls v1.SafeTLSConfig) commonConfig.TLSConfig {
	tc := commonConfig.TLSConfig{}
	tc.InsecureSkipVerify = tls.InsecureSkipVerify
	if tls.CA.Secret != nil {
		tc.CAFile = k8sfs.SecretFilename(namespace, tls.CA.Secret.Name, tls.CA.Secret.Key)
	} else if tls.CA.ConfigMap != nil {
		tc.CAFile = k8sfs.ConfigMapFilename(namespace, tls.CA.ConfigMap.Name, tls.CA.ConfigMap.Key)
	}
	if tls.Cert.Secret != nil {
		tc.CertFile = k8sfs.SecretFilename(namespace, tls.Cert.Secret.Name, tls.Cert.Secret.Key)
	} else if tls.Cert.ConfigMap != nil {
		tc.CertFile = k8sfs.ConfigMapFilename(namespace, tls.Cert.ConfigMap.Name, tls.Cert.ConfigMap.Key)
	}
	if tls.KeySecret != nil {
		tc.KeyFile = k8sfs.SecretFilename(namespace, tls.KeySecret.Name, tls.KeySecret.Key)
	}
	if tls.ServerName != "" {
		tc.ServerName = tls.ServerName
	}
	return tc
}

type relabeler struct {
	configs []*relabel.Config
	logger  log.Logger
}

func (r *relabeler) Add(cfgs ...*relabel.Config) {
	for _, cfg := range cfgs {
		// set defaults from prom defaults.
		if cfg.Action == "" {
			cfg.Action = relabel.DefaultRelabelConfig.Action
		}
		if cfg.Separator == "" {
			cfg.Separator = relabel.DefaultRelabelConfig.Separator
		}
		if cfg.Regex.Regexp == nil {
			cfg.Regex = relabel.DefaultRelabelConfig.Regex
		}
		if cfg.Replacement == "" {
			cfg.Replacement = relabel.DefaultRelabelConfig.Replacement
		}
		r.configs = append(r.configs, cfg)
	}
}

// addFromMonitoring converts from an externally generated monitoringv1 RelabelConfig
func (r *relabeler) addFromV1(cfgs ...*v1.RelabelConfig) {
	for _, c := range cfgs {
		cfg := &relabel.Config{}
		for _, l := range c.SourceLabels {
			cfg.SourceLabels = append(cfg.SourceLabels, model.LabelName(l))
		}
		if c.Separator != "" {
			cfg.Separator = c.Separator
		}
		if c.TargetLabel != "" {
			cfg.TargetLabel = c.TargetLabel
		}
		if c.Regex != "" {
			cfg.Regex = parseRegexp(c.Regex, r.logger)
		}
		if c.Modulus != 0 {
			cfg.Modulus = c.Modulus
		}
		if c.Replacement != "" {
			cfg.Replacement = c.Replacement
		}
		if c.Action != "" {
			cfg.Action = relabel.Action(c.Action)
		}
		r.configs = append(r.configs, cfg)
	}
}

func (cg *configGenerator) initRelabelings(cfg *config.ScrapeConfig) relabeler {
	r := relabeler{
		logger: cg.logger,
	}
	// Relabel prometheus job name into a meta label
	r.Add(&relabel.Config{
		SourceLabels: model.LabelNames{"job"},
		TargetLabel:  "__tmp_prometheus_job_name",
	})
	return r
}

var (
	invalidLabelCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

func sanitizeLabelName(name string) model.LabelName {
	return model.LabelName(invalidLabelCharRE.ReplaceAllString(name, "_"))
}

func (cg *configGenerator) getNamespacesFromNamespaceSelector(nsel v1.NamespaceSelector, namespace string) []string {
	if nsel.Any {
		return []string{}
	} else if len(nsel.MatchNames) == 0 {
		return []string{namespace}
	}
	return nsel.MatchNames
}

func (cg *configGenerator) generateOAuth2(oauth2 *v1.OAuth2, ns string) *commonConfig.OAuth2 {
	if oauth2 == nil {
		return nil
	}
	var err error
	oa2 := &commonConfig.OAuth2{}
	if oauth2.ClientID.Secret != nil {
		s := oauth2.ClientID.Secret
		oa2.ClientID, err = cg.secretfs.ReadSecret(ns, s.Name, s.Key)
	} else if oauth2.ClientID.ConfigMap != nil {
		cm := oauth2.ClientID.ConfigMap
		oa2.ClientID, err = cg.secretfs.ReadConfigMap(ns, cm.Name, cm.Key)
	}
	if err != nil {
		level.Error(cg.logger).Log("msg", "failed to fetch oauth clientid", "err", err)
	}
	oa2.ClientSecretFile = k8sfs.SecretFilename(ns, oauth2.ClientSecret.Name, oauth2.ClientSecret.Key)
	oa2.TokenURL = oauth2.TokenURL
	if len(oauth2.Scopes) > 0 {
		oa2.Scopes = oauth2.Scopes
	}
	if len(oauth2.EndpointParams) > 0 {
		oa2.EndpointParams = oauth2.EndpointParams
	}
	return oa2
}

func (cg *configGenerator) generateSafeAuthorization(auth *v1.SafeAuthorization, ns string) *commonConfig.Authorization {
	if auth == nil {
		return nil
	}
	az := &commonConfig.Authorization{}
	if auth.Type == "" {
		auth.Type = "Bearer"
	}
	az.Type = strings.TrimSpace(auth.Type)
	if auth.Credentials != nil {
		az.CredentialsFile = k8sfs.SecretFilename(ns, auth.Credentials.Name, auth.Credentials.Key)
	}
	return az
}