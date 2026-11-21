import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

hits = {}
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/__count":
            with lock:
                counted = sum(n for p, n in hits.items())
            body = json.dumps({"count": counted, "paths": hits}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        with lock:
            hits[path] = hits.get(path, 0) + 1
        code = 200
        if path.startswith("/status/"):
            code = int(path.rsplit("/", 1)[1])
        self.send_response(code)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, *args):
        pass


ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
