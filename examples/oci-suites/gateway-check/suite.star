load("@babelsuite/runtime", "mock", "test")

echo = mock.api(name="echo-mock")

reach = test.run(
    name="gateway-reachable",
    image="curlimages/curl:8.7.1",
    file="reach.sh",
    after=[echo],
)
