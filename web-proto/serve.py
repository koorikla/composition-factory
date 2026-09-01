#!/usr/bin/env python3
"""serve.py — stdlib-only dev server for web-proto/.

Statically serves this directory on http://127.0.0.1:5180 and proxies every
/api/* request to the live engine at http://127.0.0.1:8080 (cf serve),
forwarding method, body, and content-type, and returning the upstream status
and body verbatim. All frontend fetches are same-origin relative paths.

Run:  python3 serve.py
Open: http://127.0.0.1:5180
"""
import http.server
import os
import sys
import urllib.error
import urllib.request

PORT = 5180
UPSTREAM = "http://127.0.0.1:8080"
ROOT = os.path.dirname(os.path.abspath(__file__))


class Handler(http.server.SimpleHTTPRequestHandler):

    def end_headers(self):
        # Dev server: modules must never be cached, or edits ghost behind
        # the browser's module cache during the edit-reload loop.
        self.send_header("Cache-Control", "no-store")
        # Self-heal browsers that cached modules before no-store existed (or
        # from another server on this port): clearing the origin's HTTP cache
        # on each document load costs a few hundred loopback KB.
        if getattr(self, "_is_document", False):
            self.send_header("Clear-Site-Data", '"cache"')
        super().end_headers()
    protocol_version = "HTTP/1.1"

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=ROOT, **kwargs)

    # --- static (GET/HEAD) unless /api/* ---
    def do_GET(self):
        self._is_document = self.path in ("/", "/index.html")
        if self.path.startswith("/api/"):
            self._proxy()
        else:
            super().do_GET()

    def do_HEAD(self):
        if self.path.startswith("/api/"):
            self._proxy()
        else:
            super().do_HEAD()

    # --- everything else only makes sense against the API ---
    def do_POST(self):
        self._api_only()

    def do_PUT(self):
        self._api_only()

    def do_DELETE(self):
        self._api_only()

    def do_PATCH(self):
        self._api_only()

    def _api_only(self):
        if self.path.startswith("/api/"):
            self._proxy()
        else:
            self.send_error(405, "Method not allowed")

    def _proxy(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else None
        req = urllib.request.Request(UPSTREAM + self.path, data=body, method=self.command)
        ctype = self.headers.get("Content-Type")
        if ctype:
            req.add_header("Content-Type", ctype)
        try:
            resp = urllib.request.urlopen(req, timeout=30)
            status, data = resp.status, resp.read()
            resp_ctype = resp.headers.get("Content-Type")
        except urllib.error.HTTPError as e:
            status, data = e.code, e.read()
            resp_ctype = e.headers.get("Content-Type")
        except (urllib.error.URLError, OSError) as e:
            status = 502
            data = ('{"error":"upstream %s unreachable: %s"}' % (UPSTREAM, e)).encode()
            resp_ctype = "application/json"
        self.send_response(status)
        if resp_ctype:
            self.send_header("Content-Type", resp_ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(data)

    def log_message(self, fmt, *args):
        sys.stderr.write("%s - %s\n" % (self.address_string(), fmt % args))


def main():
    server = http.server.ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print("serving %s on http://127.0.0.1:%d (API proxied to %s)" % (ROOT, PORT, UPSTREAM))
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
