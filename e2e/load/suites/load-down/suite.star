load("@babelsuite/runtime", "traffic")

load_step = traffic.smoke(name="load", target="http://nothing.invalid:8000")
