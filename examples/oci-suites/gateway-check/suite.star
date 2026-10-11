load("@babelsuite/runtime", "service", "test")

echo = service.mock(name="echo-mock")

reach = test.run(
    name="gateway-reachable",
    image="curlimages/curl:8.7.1",
    file="reach.sh",
    after=[echo],
)
