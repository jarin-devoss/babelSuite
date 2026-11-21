load("@babelsuite/runtime", "mock", "traffic")

api = mock.api(name="load-api")
load_step = traffic.baseline(name="load", target="https://load-mock.mock.internal", plan="greeting.star", after=[api])
