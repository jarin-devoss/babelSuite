load("@babelsuite/runtime", "traffic")

load_step = traffic.smoke(name="load", target="http://ghost:8000")
