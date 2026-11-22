// Control system simulation plugin config schema (scipy.signal / python-control backend).
// Validates the config of a step that calls one of this plugin's operations,
// as loaded with load("@plugins/control-sim", ...). The runner checks it before dispatch.

sim_url:             string              // HTTP URL of the scipy control simulation microservice
numerator:           [...number]         // transfer function numerator coefficients [b0, b1, ...]
denominator:         [...number]         // transfer function denominator coefficients [a0, a1, ...]
analysis:            "step" | "impulse" | "bode" | *"step"
time_end:            number & >0 | *10  // simulation end time in seconds
max_settling_time:   number | *null     // finding if settling time (±2% of final) exceeds this (s)
max_overshoot_pct:   number | *null     // finding if % overshoot exceeds this
min_dc_gain:         number | *null     // finding if DC gain (steady-state / input) falls below this
max_dc_gain:         number | *null     // finding if DC gain exceeds this
severity:            "warn" | "critical" | *"critical"

// impulse_response: how close to zero the response must settle by time_end.
max_residual:        number | *null

// frequency_response: minimum stability margins from the Bode analysis.
min_gain_margin_db:   number | *null
min_phase_margin_deg: number | *null
