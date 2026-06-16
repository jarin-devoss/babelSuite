package suites

// PluginSpec carries the registered plugin name and operation used in a plugin step.
type PluginSpec struct {
	Name   string `json:"name"`
	Op     string `json:"op,omitempty"`
	Config any    `json:"config,omitempty"`
}

// SpiceSimSpec holds config for a spice-sim plugin step.
type SpiceSimSpec struct {
	SimURL    string  `json:"simUrl"`
	Netlist   string  `json:"netlist"`
	ProbeNode string  `json:"probeNode"`
	StepMS    float64 `json:"stepMs"`
	EndMS     float64 `json:"endMs"`
	MaxRiseMS float64 `json:"maxRiseMs,omitempty"`
	MaxV      float64 `json:"maxV,omitempty"`
	MinV      float64 `json:"minV,omitempty"`
	Severity  string  `json:"severity"`
}

// VerilogSimSpec holds config for a verilog-sim plugin step.
type VerilogSimSpec struct {
	SimURL    string `json:"simUrl"`
	TopModule string `json:"topModule"`
	Verilog   string `json:"verilog"`
	Testbench string `json:"testbench"`
	TimeoutNS int    `json:"timeoutNs"`
	MaxErrors int    `json:"maxErrors"`
	Severity  string `json:"severity"`
}

// ControlSimSpec holds config for a control-sim plugin step.
type ControlSimSpec struct {
	SimURL         string    `json:"simUrl"`
	Numerator      []float64 `json:"numerator"`
	Denominator    []float64 `json:"denominator"`
	Analysis       string    `json:"analysis"`
	TimeEnd        float64   `json:"timeEnd"`
	MaxSettlingS   float64   `json:"maxSettlingS,omitempty"`
	MaxOvershootPct float64  `json:"maxOvershootPct,omitempty"`
	MinDCGain      float64   `json:"minDcGain,omitempty"`
	MaxDCGain      float64   `json:"maxDcGain,omitempty"`
	Severity       string    `json:"severity"`
}

// ConsumerLagSpec holds config for a consumer-lag plugin step.
type ConsumerLagSpec struct {
	KafkaRestURL string  `json:"kafkaRestUrl"`
	Group        string  `json:"group"`
	MaxLag       int     `json:"maxLag"`
	Severity     string  `json:"severity"`
}

// DLQInspectorSpec holds config for a dlq-inspector plugin step.
type DLQInspectorSpec struct {
	KafkaRestURL string `json:"kafkaRestUrl"`
	Topic        string `json:"topic"`
	MaxMessages  int    `json:"maxMessages"`
	Severity     string `json:"severity"`
}

// ShadowDiffSpec holds config for a shadow-diff plugin step.
type ShadowDiffSpec struct {
	Primary      string   `json:"primary"`
	Shadow       string   `json:"shadow"`
	Threshold    float64  `json:"threshold"`
	IgnoreFields []string `json:"ignoreFields,omitempty"`
	Severity     string   `json:"severity"`
}

// PIIScannerSpec holds config for a pii-scanner plugin step.
type PIIScannerSpec struct {
	Target   string   `json:"target"`
	Patterns []string `json:"patterns,omitempty"`
	Severity string   `json:"severity"`
}

// CanaryValidatorSpec holds config for a canary-validator plugin step.
type CanaryValidatorSpec struct {
	Target        string  `json:"target"`
	CanaryHeader  string  `json:"canaryHeader"`
	ExpectedRatio float64 `json:"expectedRatio"`
	Tolerance     float64 `json:"tolerance"`
	SampleSize    int     `json:"sampleSize"`
	Severity      string  `json:"severity"`
}

// SchemaCompatSpec holds config for a schema-compat plugin step.
type SchemaCompatSpec struct {
	RegistryURL string `json:"registryUrl"`
	Subject     string `json:"subject"`
	Mode        string `json:"mode"`
	NewSchema   string `json:"newSchema"`
	Severity    string `json:"severity"`
}
