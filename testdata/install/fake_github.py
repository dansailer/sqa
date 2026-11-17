#!/usr/bin/env python3
"""GitHub Releases API stand-in for install.sh tests. Never logs headers."""

from __future__ import annotations

import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(os.environ["FAKE_RELEASE_DIR"])
HOST = os.environ.get("FAKE_HOST", "127.0.0.1")
PORT = int(os.environ["FAKE_PORT"])
TOKEN = os.environ.get("FAKE_TOKEN", "")
TAG = os.environ.get("FAKE_TAG", "v0.0.0")
VER = TAG[1:] if TAG.startswith("v") else TAG
ARCHIVE = f"sqa_{VER}_linux_amd64.tar.gz"
ASSETS = {ARCHIVE: 101, "checksums.txt": 102}


def release_body(base: str) -> dict:
    assets = []
    for name, asset_id in ASSETS.items():
        assets.append(
            {
                "url": f"{base}/repos/dansailer/sqa/releases/assets/{asset_id}",
                "id": asset_id,
                "name": name,
                "browser_download_url": f"{base}/download/{TAG}/{name}",
            }
        )
    return {
        "url": f"{base}/repos/dansailer/sqa/releases/1",
        "tag_name": TAG,
        "assets": assets,
    }


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:
        # Path and status only — never headers or tokens.
        sys.stderr.write("%s %s\n" % (self.requestline, args[-1] if args else ""))

    def _auth_ok(self) -> bool:
        if not TOKEN:
            return True
        return self.headers.get("Authorization", "") == "Bearer " + TOKEN

    def do_GET(self) -> None:  # noqa: N802
        path = self.path.split("?", 1)[0]
        if not self._auth_ok():
            self.send_error(401, "Bad credentials")
            return
        base = f"http://{HOST}:{PORT}"
        if path in (
            "/repos/dansailer/sqa/releases/latest",
            f"/repos/dansailer/sqa/releases/tags/{TAG}",
        ):
            body = json.dumps(release_body(base)).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        for name, asset_id in ASSETS.items():
            if path == f"/repos/dansailer/sqa/releases/assets/{asset_id}":
                accept = self.headers.get("Accept", "")
                if "application/octet-stream" not in accept:
                    self.send_error(400, "expected Accept: application/octet-stream")
                    return
                data = (ROOT / name).read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "application/octet-stream")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                return
            if path in (f"/download/{TAG}/{name}", f"/{name}"):
                data = (ROOT / name).read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "application/octet-stream")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
                return
        self.send_error(404, path)


if __name__ == "__main__":
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"ready {HOST}:{PORT}", flush=True)
    httpd.serve_forever()
