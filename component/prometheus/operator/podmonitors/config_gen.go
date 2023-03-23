package podmonitors

// SEE https://github.com/prometheus-operator/prometheus-operator/blob/main/pkg/prometheus/promcfg.go

import (
	"fmt"
	"regexp"

	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	commonConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	promk8s "github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/model/relabel"
)

type configGenerator struct {
	config *Arguments
}

// the k8s sd config is mostly dependent on our local config for accessing the kubernetes cluster.
// if undefined it will default to an in-cluster config
func (cg *configGenerator) generateK8SSDConfig(namespaceSelector v1.NamespaceSelector, namespace string, role promk8s.Role, attachMetadata *v1.AttachMetadata) *promk8s.SDConfig {
	cfg := &promk8s.SDConfig{
		Role: role,
	}
	namespaces := cg.getNamespacesFromNamespaceSelector(namespaceSelector, namespace)
	if len(namespaces) != 0 {
		cfg.NamespaceDiscovery.Names = namespaces
	}
	client := cg.config.Client
	if client.KubeConfig != "" {
		cfg.KubeConfig = client.KubeConfig
	}
	if client.APIServer.URL != nil {
		hCfg := client.HTTPClientConfig
		cfg.APIServer = client.APIServer.Convert()

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

func (cg *configGenerator) GenerateSafeTLS(namespace string, tls v1.SafeTLSConfig) (commonConfig.TLSConfig, error) {
	tc := commonConfig.TLSConfig{}
	tc.InsecureSkipVerify = tls.InsecureSkipVerify

	if tls.CA.Secret != nil || tls.CA.ConfigMap != nil {
		return tc, fmt.Errorf("loading ca certs no supported yet")
	}
	if tls.Cert.Secret != nil || tls.Cert.ConfigMap != nil {
		return tc, fmt.Errorf("loading tls certs no supported yet")
	}
	if tls.KeySecret != nil {
		return tc, fmt.Errorf("loading tls certs no supported yet")
	}
	if tls.ServerName != "" {
		tc.ServerName = tls.ServerName
	}
	return tc, nil
}

type relabeler struct {
	configs []*relabel.Config
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

// addFromV1 converts from an externally generated monitoringv1 RelabelConfig. Used for converting relabel rules generated by external package
func (r *relabeler) addFromV1(cfgs ...*v1.RelabelConfig) (err error) {
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
			cfg.Regex, err = relabel.NewRegexp(c.Regex)
			if err != nil {
				return err
			}
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
	return nil
}

func (cg *configGenerator) initRelabelings() relabeler {
	r := relabeler{}
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

func (cg *configGenerator) GenerateOAuth2(oauth2 *v1.OAuth2, namespace string) (*commonConfig.OAuth2, error) {
	if oauth2 == nil {
		return nil, nil
	}
	return nil, fmt.Errorf("oauth2 not supported yet")
}

func (cg *configGenerator) GenerateSafeAuthorization(auth *v1.SafeAuthorization, ns string) (*commonConfig.Authorization, error) {
	if auth == nil {
		return nil, nil
	}
	return nil, fmt.Errorf("authorization not supported yet")
}