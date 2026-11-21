load("@babelsuite/runtime", "test")

tick = test.run(name="tick", image="busybox:latest", file="tick.sh")
