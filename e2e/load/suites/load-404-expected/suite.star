load("@babelsuite/runtime", "service", "test", "traffic")

counter = service.run(name="counter", image="python:3.12-alpine", file="counter.py")
ready = test.run(
    name="ready",
    image="curlimages/curl:8.7.1",
    commands=["until curl -sf http://counter:8000/__count >/dev/null; do sleep 1; done"],
    after=[counter],
)
burst = traffic.baseline(name="load", target="http://counter:8000", plan="expect_404.star", after=[ready])
