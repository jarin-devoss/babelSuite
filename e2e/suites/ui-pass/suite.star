load("@babelsuite/runtime", "log", "task", "test")

start = log.info("UI-PASS starting")
echo = task.run(name="echo", image="busybox:latest", commands=["echo UI-PASS value=$E2E_VAR"], after=[start])
verify = test.run(name="verify", image="busybox:latest", commands=["echo UI-PASS verified"], expect_logs="UI-PASS verified", after=[echo])
