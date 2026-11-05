"""Release server for the install script tests.

Serves ROOT on 127.0.0.1 with the layout of GitHub releases, one tree per
test case:

  /<case>/latest                     302 to /<case>/tag/<contents of ROOT/<case>/latest>
  /<case>/download/<version>/<asset> ROOT/<case>/download/<version>/<asset>

Next to an asset, <asset>.truncate makes the response stop halfway through
the advertised length, and <asset>.redirect (holding a URL) answers with a
302 to that URL. Every request path is appended to ROOT/<case>/requests.log.

usage: python3 -I serve.py ROOT PORTFILE
"""

import http.server
import os
import sys


class Handler(http.server.BaseHTTPRequestHandler):
    root = ""

    def log_message(self, *args):
        pass

    def do_HEAD(self):
        self.handle_request(body=False)

    def do_GET(self):
        self.handle_request(body=True)

    def handle_request(self, body):
        path = self.path.split("?", 1)[0]
        parts = [p for p in path.split("/") if p]
        if not parts or any(p in (".", "..") or p.startswith(".") for p in parts):
            return self.send_error(404)
        case_dir = os.path.join(self.root, parts[0])
        if not os.path.isdir(case_dir):
            return self.send_error(404)
        with open(os.path.join(case_dir, "requests.log"), "a") as log:
            log.write(path + "\n")

        if parts[1:] == ["latest"]:
            latest = os.path.join(case_dir, "latest")
            if not os.path.isfile(latest):
                return self.send_error(404)
            with open(latest) as f:
                tag = f.read().strip()
            return self.redirect("/%s/tag/%s" % (parts[0], tag))

        if len(parts) != 4 or parts[1] != "download":
            return self.send_error(404)
        asset = os.path.join(case_dir, "download", parts[2], parts[3])
        if os.path.isfile(asset + ".redirect"):
            with open(asset + ".redirect") as f:
                return self.redirect(f.read().strip())
        if not os.path.isfile(asset):
            return self.send_error(404)
        with open(asset, "rb") as f:
            data = f.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if not body:
            return
        if os.path.isfile(asset + ".truncate"):
            self.wfile.write(data[: len(data) // 2])
            self.wfile.flush()
            self.close_connection = True
            return
        self.wfile.write(data)

    def redirect(self, location):
        self.send_response(302)
        self.send_header("Location", location)
        self.send_header("Content-Length", "0")
        self.end_headers()


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: serve.py ROOT PORTFILE")
    Handler.root = os.path.abspath(sys.argv[1])
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    tmp = sys.argv[2] + ".tmp"
    with open(tmp, "w") as f:
        f.write(str(server.server_address[1]))
    os.rename(tmp, sys.argv[2])
    server.serve_forever()


if __name__ == "__main__":
    main()
