#!/usr/bin/env python3
"""github.com release-page stand-in for install.sh tests.

Serves /dansailer/sqa/releases/latest as a redirect to
/dansailer/sqa/releases/tag/<FAKE_TAG> (or to /releases when FAKE_TAG is
empty, as github.com does for a repository without releases) and the assets
under /dansailer/sqa/releases/download/<tag>/. Any request carrying an
Authorization header is refused: the installer must never send a token.
"""

from __future__ import annotations

import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(os.environ["FAKE_RELEASE_DIR"])
HOST = os.environ.get("FAKE_HOST", "127.0.0.1")
PORT = int(os.environ["FAKE_PORT"])
TAG = os.environ.get("FAKE_TAG", "v0.0.0")
REPO = "/dansailer/sqa/releases"


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:
        # Request line and status only, never headers.
        sys.stderr.write("%s %s\n" % (self.requestline, args[-1] if args else ""))

    def _redirect(self, to: str) -> None:
        self.send_response(302)
        self.send_header("Location", f"http://{HOST}:{PORT}{to}")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def _send(self, data: bytes) -> None:
        self.send_response(200)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self) -> None:  # noqa: N802
        path = self.path.split("?", 1)[0]
        if "Authorization" in self.headers:
            sys.stderr.write("AUTHORIZATION HEADER RECEIVED\n")
            self.send_error(400, "unexpected Authorization header")
            return
        if path == f"{REPO}/latest":
            self._redirect(f"{REPO}/tag/{TAG}" if TAG else REPO)
            return
        if path == REPO or path == f"{REPO}/tag/{TAG}":
            self._send(b"<html>release page</html>")
            return
        prefix = f"{REPO}/download/{TAG}/"
        if TAG and path.startswith(prefix):
            f = ROOT / path[len(prefix):]
            if f.is_file():
                self._send(f.read_bytes())
                return
        self.send_error(404, path)


if __name__ == "__main__":
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"ready {HOST}:{PORT}", flush=True)
    httpd.serve_forever()
