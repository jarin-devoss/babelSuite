load("@babelsuite/runtime", "service")
load("@plugins/skill-eval", "trigger", "arguments", "outcome", "uplift",
     "consistency", "sequence", "latency", "monitor")

# Exercises every skill-eval operation against a mocked agent. The mock answers
# by model name — router-mock always calls issue_refund, planner-mock calls
# lookup_order and stops, judge-mock grades answers — so each step has
# a known score. Steps mix cases the mock satisfies with cases it does not, and
# set the threshold just under the expected score: the step passes, and the
# findings for the failing cases still show in the run.
#
# This proves the plugin scores correctly; it does not evaluate a model. The
# ai-skill-eval-live suite runs the same kind of cases against a real one.

AGENT_URL = "http://agent-model:9080/v1/chat/completions"

SKILLS = [
    {
        "name":         "lookup_order",
        "description":  "Look up an order's status, items and delivery date by order id.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"}},"required":["order_id"]}',
    },
    {
        "name":         "issue_refund",
        "description":  "Refund an order that arrived damaged, wrong or not at all.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"},"reason":{"type":"string","enum":["damaged","wrong_item","not_delivered"]}},"required":["order_id","reason"]}',
        "instructions": "You are handling a refund. Confirm the order id back to the customer and say when the money arrives: refunds reach the original payment method within 5 business days. Never offer discount codes.",
    },
    {
        "name":         "track_shipment",
        "description":  "Report where a parcel is right now and when it will arrive.",
        "parameters":   '{"type":"object","properties":{"order_id":{"type":"integer"}},"required":["order_id"]}',
    },
]

agent_model = service.mock(name = "agent-model")

# 3 of 4 cases route correctly; the parcel question goes to issue_refund
# instead of track_shipment and is reported as skill.misrouted.
routing = trigger(
    name      = "routing",
    skills    = SKILLS,
    model_url = AGENT_URL,
    model     = "router-mock",
    threshold = 0.75,
    cases     = [
        {"prompt": "Order 1001 arrived smashed, I want my money back.", "expect": "issue_refund"},
        {"prompt": "Refund order 1001 please, the box was crushed.",   "expect": "issue_refund"},
        {"prompt": "The mug in order 1001 is broken. Refund it.",      "expect": "issue_refund"},
        {"prompt": "Where is my parcel for order 1001 right now?",     "expect": "track_shipment"},
    ],
    after     = [agent_model],
)

# Small talk should call no skill. monitor reports the skill.false_trigger at
# warn level and never fails the suite.
small_talk = monitor(
    name      = "small-talk",
    skills    = SKILLS,
    model_url = AGENT_URL,
    model     = "router-mock",
    cases     = [
        {"prompt": "Thanks, that's all for today!", "expect": None},
    ],
    after     = [agent_model],
)

# Both calls pass the schema check against issue_refund's parameters. The
# first case pins the arguments the mock sends; the second expects a different
# reason and is reported as skill.wrong_args.
refund_args = arguments(
    name      = "refund-arguments",
    skills    = SKILLS,
    model_url = AGENT_URL,
    model     = "router-mock",
    threshold = 0.5,
    cases     = [
        {"prompt": "Order 1001 arrived smashed, refund it.", "expect": "issue_refund",
         "args": {"order_id": 1001, "reason": "damaged"}},
        {"prompt": "Order 1001 never showed up, refund it.", "expect": "issue_refund",
         "args": {"order_id": 1001, "reason": "not_delivered"}},
    ],
    after     = [routing],
)

# The mock never varies, so every sample agrees. Against a real model this is
# the step that catches routing that only works some of the time.
refund_stability = consistency(
    name        = "refund-stability",
    skills      = SKILLS,
    model_url   = AGENT_URL,
    model       = "router-mock",
    samples     = 3,
    temperature = 0.8,
    cases       = [
        {"prompt": "Order 1001 arrived smashed, I want my money back.", "expect": "issue_refund"},
    ],
    after       = [routing],
)

# planner-mock looks the order up and stops. The first case only asks for the
# lookup and passes; the second needs the refund after it, and the missing call
# is reported as skill.wrong_sequence — the agent that passes trigger while
# leaving the job undone.
refund_plan = sequence(
    name      = "refund-plan",
    skills    = SKILLS,
    model_url = AGENT_URL,
    model     = "planner-mock",
    threshold = 0.5,
    cases     = [
        {"prompt": "What's in order 1001?",
         "sequence": ["lookup_order"]},
        {"prompt": "Check order 1001 and refund it if it was damaged.",
         "sequence": ["lookup_order", "issue_refund"]},
    ],
    after     = [agent_model],
)

# A mock answers in milliseconds; the budget is what a real agent would get.
refund_latency = latency(
    name           = "refund-latency",
    skills         = SKILLS,
    model_url      = AGENT_URL,
    model          = "router-mock",
    max_latency_ms = 2000,
    cases          = [
        {"prompt": "Order 1001 arrived smashed, refund it.", "expect": "issue_refund"},
    ],
    after          = [routing],
)

# issue_refund's instructions are loaded as the system prompt and judge-mock
# grades the answer. The judge answers the first case's two criteria; the
# second case also asks about discount codes, which the judge leaves
# unanswered, so it is reported as skill.unanswered.
refund_answers = outcome(
    name        = "refund-answers",
    skills      = SKILLS,
    model_url   = AGENT_URL,
    model       = "router-mock",
    judge_model = "judge-mock",
    threshold   = 0.5,
    cases       = [
        {"prompt": "Order 1001 arrived smashed. When do I get my money?", "expect": "issue_refund",
         "criteria": ["says when the refund arrives", "names the order"]},
        {"prompt": "Refund order 1001 and give me something for the trouble.", "expect": "issue_refund",
         "criteria": ["says when the refund arrives", "does not offer a discount code"]},
    ],
    after       = [refund_args],
)

# Is the skill worth loading at all? Each case is answered with issue_refund's
# instructions and without them, and both answers are graded. The mock answers
# the same either way, so the plugin correctly reports skill.no_uplift — the
# base model "already does this". warn keeps that finding from failing the run;
# against a real model, critical makes it a gate on the skill earning its place.
refund_uplift = uplift(
    name        = "refund-uplift",
    skills      = SKILLS,
    model_url   = AGENT_URL,
    model       = "router-mock",
    judge_model = "judge-mock",
    severity    = "warn",
    cases       = [
        {"prompt": "Order 1001 arrived smashed. When do I get my money?", "expect": "issue_refund",
         "criteria": ["says when the refund arrives", "names the order"]},
    ],
    after       = [refund_answers],
)
