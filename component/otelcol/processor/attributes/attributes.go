// Package attributes provides an otelcol.processor.attributes component.
package attributes

import (
	"github.com/grafana/agent/component"
	"github.com/grafana/agent/component/otelcol"
	"github.com/grafana/agent/component/otelcol/processor"
	"github.com/mitchellh/mapstructure"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/attributesprocessor"
	otelcomponent "go.opentelemetry.io/collector/component"
	otelconfig "go.opentelemetry.io/collector/config"
)

func init() {
	component.Register(component.Registration{
		Name:    "otelcol.processor.attributes",
		Args:    Arguments{},
		Exports: otelcol.ConsumerExports{},

		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			fact := attributesprocessor.NewFactory()
			return processor.New(opts, fact, args.(Arguments))
		},
	})
}

// Arguments configures the otelcol.processor.attributes component.
type Arguments struct {
	//TODO: Fix this later
	// Match otelcol.MatchConfig `river:"match,block,optional"`

	//TODO: Squash this
	Actions otelcol.AttrActionSettings `river:"actions,block,optional"`

	// Output configures where to send processed data. Required.
	Output *otelcol.ConsumerArguments `river:"output,block"`
}

var (
	_ processor.Arguments = Arguments{}
)

// Convert implements processor.Arguments.
func (args Arguments) Convert() otelconfig.Processor {
	input := make(map[string]interface{})

	if actions := args.Actions.Convert(); len(actions) > 0 {
		input["actions"] = actions
	}

	var result attributesprocessor.Config
	err := mapstructure.Decode(input, &result)

	if err != nil {
		//TODO: Don't panic. Return error or log error and return nil?
		panic(err)
	}

	result.ProcessorSettings = otelconfig.NewProcessorSettings(otelconfig.NewComponentID("attributes"))

	return &result
}

// Extensions implements processor.Arguments.
func (args Arguments) Extensions() map[otelconfig.ComponentID]otelcomponent.Extension {
	return nil
}

// Exporters implements processor.Arguments.
func (args Arguments) Exporters() map[otelconfig.DataType]map[otelconfig.ComponentID]otelcomponent.Exporter {
	return nil
}

// NextConsumers implements processor.Arguments.
func (args Arguments) NextConsumers() *otelcol.ConsumerArguments {
	return args.Output
}
