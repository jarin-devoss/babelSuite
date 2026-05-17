package suites

type SecuritySpec struct {
	Variant        string              `json:"variant"`
	Target         string              `json:"target,omitempty"`
	Technique      string              `json:"technique,omitempty"`
	FloodPath      string              `json:"floodPath,omitempty"`
	FloodRate      float64             `json:"floodRate,omitempty"`
	FloodDuration  float64             `json:"floodDuration,omitempty"`
	FloodThrottle  bool                `json:"floodThrottle,omitempty"`
	Thresholds     []SecurityThreshold `json:"thresholds,omitempty"`
}

type SecurityThreshold struct {
	Metric   string  `json:"metric"`
	Operator string  `json:"operator"`
	Value    float64 `json:"value"`
}
