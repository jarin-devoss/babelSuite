// Analog circuit simulation plugin config schema (PySpice / Ngspice backend).
// Validates configuration passed to plugin.run(plugin="babelsuite-spice-sim", config={...}).

sim_url:       string                          // HTTP URL of the PySpice simulation microservice
netlist:       string                          // SPICE netlist as a string (standard .cir format)
analysis:      "transient" | "dc" | "ac" | *"transient"
step_time_ms:  number & >0 | *0.1             // transient: time step in milliseconds
end_time_ms:   number & >0 | *10              // transient: simulation end time in milliseconds
probe_node:    string | *"out"                 // node name to measure (matches netlist node label)
max_voltage:   number | *null                  // finding if peak voltage at probe_node exceeds this
min_voltage:   number | *null                  // finding if min voltage at probe_node falls below this
max_rise_ms:   number | *null                  // finding if 10–90% rise time exceeds this (ms)
severity:      "warn" | "critical" | *"critical"
