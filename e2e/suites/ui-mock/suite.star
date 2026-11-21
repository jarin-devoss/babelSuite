load("@babelsuite/runtime", "mock", "test")

api = mock.api(name="ui-api")
call = test.run(name="call", image="curlimages/curl:8.7.1", file="call.sh", after=[api])
