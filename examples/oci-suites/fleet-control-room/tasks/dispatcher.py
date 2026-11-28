"""The route dispatcher fleet-control-room tests.

It assigns routes to vehicles. For each assignment it asks the geo-fence service
whether the zone may be entered, then asks the route planner (gRPC; the call is
gRPC-Web, framed by hand because the standard library has no gRPC client) for an
assignment, and keeps what came of it. It also forwards vehicle telemetry to the
telemetry service. All three are mocks, reached by name.

  POST /routes/assign   {routeId, vehicleId, zone, region}
                        201 {assignmentId, ...} | 400 | 403 restricted zone | 502 planner or geo-fence
  GET  /routes/{id}     a stored assignment
  POST /telemetry/events {vehicleId, battery, speed}   202 | 502
  GET  /fleet/status    {status, regions, assignments, telemetryFrames}
  GET  /health

The rule the suite exists to protect: a vehicle is never routed into a zone the
geo-fence service says is restricted, and the planner is not even asked (see
assign()). Take that check out and geo-fence-audit, which asks for a route through
the restricted airport apron, fails.
"""
import json
import os
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

GATEWAY = os.environ.get("GATEWAY_URL", "http://babelsuite-gateway:9080").rstrip("/")
GEOFENCE = os.environ.get("GEOFENCE_URL", "http://geo-fence-mock:9080").rstrip("/")
TELEMETRY = os.environ.get("TELEMETRY_URL", "http://telemetry-mock:9080").rstrip("/")
REGIONS = [r for r in os.environ.get("REGIONS", "eu-west,us-east,ap-south").split(",") if r]
METHOD = "/fleet_control_room.v1.RoutePlanner/AssignRoute"

lock = threading.Lock()
assignments = {}
frames = [0]


# -- protobuf and gRPC-Web, by hand ------------------------------------------
def varint(number):
    out = b""
    while True:
        low, number = number & 0x7F, number >> 7
        out += bytes([low | 0x80 if number else low])
        if not number:
            return out


def encode_request(route, vehicle, zone):
    message = b""
    for number, text in ((1, route), (2, vehicle), (3, zone)):
        raw = text.encode()
        message += varint(number << 3 | 2) + varint(len(raw)) + raw
    return b"\x00" + len(message).to_bytes(4, "big") + message


def read_varint(data, at):
    shift = value = 0
    while True:
        byte = data[at]
        at += 1
        value |= (byte & 0x7F) << shift
        if not byte & 0x80:
            return value, at
        shift += 7


def decode_response(message):
    out, at = {}, 0
    while at < len(message):
        tag, at = read_varint(message, at)
        number, wire = tag >> 3, tag & 7
        if wire == 0:
            out[number], at = read_varint(message, at)
        elif wire == 2:
            size, at = read_varint(message, at)
            out[number] = message[at:at + size].decode()
            at += size
        else:
            raise ValueError("unexpected wire type %d" % wire)
    return {"status": out.get(1, ""), "assignmentId": out.get(2, ""), "plannerRevision": out.get(3, "")}


def plan(route, vehicle, zone):
    """The planner's assignment, or (None, why) when it gave none."""
    request = urllib.request.Request(
        GATEWAY + METHOD, data=encode_request(route, vehicle, zone), method="POST",
        headers={"Content-Type": "application/grpc-web+proto", "X-Grpc-Web": "1", "Accept": "application/grpc-web+proto"})
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            body, header_status = reply.read(), reply.headers.get("grpc-status")
    except (urllib.error.URLError, OSError) as err:
        return None, "the route planner did not answer: %s" % err
    message, status, at = None, header_status or "0", 0
    while at + 5 <= len(body):
        flag, size = body[at], int.from_bytes(body[at + 1:at + 5], "big")
        payload = body[at + 5:at + 5 + size]
        at += 5 + size
        if flag & 0x80:
            for line in payload.decode(errors="replace").split("\r\n"):
                if line.lower().startswith("grpc-status:"):
                    status = line.split(":", 1)[1].strip()
        else:
            message = payload
    if status != "0" or message is None:
        return None, "the route planner has no plan for this route (grpc-status %s)" % status
    return decode_response(message), ""


# -- the other services --------------------------------------------------------
def post_json(url, body):
    request = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST", headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=10) as reply:
            return reply.status, json.loads(reply.read() or b"{}")
    except urllib.error.HTTPError as err:
        return err.code, {}
    except (urllib.error.URLError, OSError, ValueError):
        return 0, {}


def zone_allowed(zone):
    status, answer = post_json(GEOFENCE + "/zones/check", {"zone": zone})
    if status != 200 or "allowed" not in answer:
        return None, "the geo-fence service did not answer"
    return answer["allowed"], answer.get("reason", "")


# -- the rule ----------------------------------------------------------------
def assign(body):
    if not isinstance(body, dict) or not all(isinstance(body.get(k), str) and body[k] for k in ("routeId", "vehicleId", "zone", "region")):
        return 400, {"error": "routeId, vehicleId, zone and region are required"}
    if body["region"] not in REGIONS:
        return 400, {"error": "region must be one of %s" % ",".join(REGIONS)}
    allowed, why = zone_allowed(body["zone"])
    if allowed is None:
        return 502, {"error": why}
    if not allowed:  # the rule: no route into a restricted zone, and no call to the planner
        print("BOUNDARY_CHECK refused %s into %s" % (body["vehicleId"], body["zone"]), flush=True)
        return 403, {"error": "zone %s is restricted: %s" % (body["zone"], why)}
    answer, why = plan(body["routeId"], body["vehicleId"], body["zone"])
    if answer is None:
        print("PLANNER_REFUSED " + why, flush=True)
        return 502, {"error": why}
    record = {"routeId": body["routeId"], "vehicleId": body["vehicleId"], "zone": body["zone"], "region": body["region"],
              "assignmentId": answer["assignmentId"], "plannerRevision": answer["plannerRevision"]}
    with lock:
        assignments[body["routeId"]] = record
    return 201, record


def telemetry(body):
    if not isinstance(body, dict) or not body.get("vehicleId"):
        return 400, {"error": "vehicleId is required"}
    status, answer = post_json(TELEMETRY + "/telemetry/events", body)
    if status not in (200, 202):
        return 502, {"error": "the telemetry service refused the frame (%s)" % status}
    with lock:
        frames[0] += 1
    return 202, answer


class Server(ThreadingHTTPServer):
    request_queue_size = 256
    daemon_threads = True


class Handler(BaseHTTPRequestHandler):
    server_version = "dispatcher"

    def reply(self, status, body):
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path == "/health":
            return self.reply(200, {"status": "ok"})
        if self.path == "/fleet/status":
            with lock:
                return self.reply(200, {"status": "dispatching", "regions": REGIONS, "assignments": len(assignments), "telemetryFrames": frames[0]})
        if self.path.startswith("/routes/"):
            with lock:
                record = assignments.get(self.path.rsplit("/", 1)[1])
            return self.reply(*((200, record) if record else (404, {"error": "no such route"})))
        self.reply(404, {"error": "not found"})

    def do_POST(self):
        try:
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length") or 0)) or b"null")
        except ValueError:
            body = None
        if self.path == "/routes/assign":
            return self.reply(*assign(body))
        if self.path == "/telemetry/events":
            return self.reply(*telemetry(body))
        self.reply(404, {"error": "not found"})

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    print("dispatcher on %d: regions %s" % (port, REGIONS), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()
