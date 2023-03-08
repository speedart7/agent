package config

import (
	"fmt"
	"reflect"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/agent/pkg/build"
	promconfig "github.com/prometheus/common/config"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientArguments controls how to connect to a Kubernetes cluster.
type ClientArguments struct {
	APIServer        URL              `river:"api_server,attr,optional"`
	KubeConfig       string           `river:"kubeconfig_file,attr,optional"`
	HTTPClientConfig HTTPClientConfig `river:",squash"`
}

// UnmarshalRiver unmarshals ClientArguments and performs validations.
func (args *ClientArguments) UnmarshalRiver(f func(interface{}) error) error {
	type arguments ClientArguments
	if err := f((*arguments)(args)); err != nil {
		return err
	}

	if args.APIServer.URL != nil && args.KubeConfig != "" {
		return fmt.Errorf("only one of api_server and kubeconfig_file can be set")
	}
	if args.KubeConfig != "" && !reflect.DeepEqual(args.HTTPClientConfig, DefaultHTTPClientConfig) {
		return fmt.Errorf("custom HTTP client configuration is not allowed when kubeconfig_file is set")
	}
	if args.APIServer.URL == nil && !reflect.DeepEqual(args.HTTPClientConfig, DefaultHTTPClientConfig) {
		return fmt.Errorf("api_server must be set when custom HTTP client configuration is provided")
	}

	// We must explicitly Validate because HTTPClientConfig is squashed and it won't run otherwise
	return args.HTTPClientConfig.Validate()
}

// BuildRESTConfig converts ClientArguments to a Kubernetes REST config.
func (args *ClientArguments) BuildRESTConfig(l log.Logger) (*rest.Config, error) {
	var (
		cfg *rest.Config
		err error
	)

	switch {
	case args.KubeConfig != "":
		cfg, err = clientcmd.BuildConfigFromFlags("", args.KubeConfig)
		if err != nil {
			return nil, err
		}

	case args.APIServer.URL == nil:
		// Use in-cluster config.
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		level.Info(l).Log("msg", "Using pod service account via in-cluster config")

	default:
		rt, err := promconfig.NewRoundTripperFromConfig(*args.HTTPClientConfig.Convert(), "loki.source.kubernetes")
		if err != nil {
			return nil, err
		}
		cfg = &rest.Config{
			Host:      args.APIServer.String(),
			Transport: rt,
		}
	}

	cfg.UserAgent = fmt.Sprintf("GrafanaAgent/%s", build.Version)
	cfg.ContentType = "application/vnd.kubernetes.protobuf"

	return cfg, nil
}
