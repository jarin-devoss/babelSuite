load("@babelsuite/runtime", "log", "test")

bad = test.run(name="bad", image="busybox:latest", commands=["echo UI-CONTINUE this step fails", "exit 1"], continue_on_failure=True)
done = log.info("UI-CONTINUE the run went on", after=[bad])
