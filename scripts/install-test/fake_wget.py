"""Stand-in for wget in the install script tests, over plain http only.

usage: fake_wget.py gnu|busybox LOGFILE [wget arguments]

gnu takes -q, -S, -O FILE, --spider, --https-only and --max-redirect=N and,
like GNU wget, follows redirects to any scheme (--https-only only matters in
recursive mode). busybox takes -q, -S, -O FILE and --spider and rejects every
other option, as BusyBox wget does with GNU long options. Both print server
responses with -S, one indented header per line, and exit 8 on an HTTP error
and 4 on a network failure or a short body.
"""

import http.client
import sys
import urllib.parse

HELP = {
    "gnu": "GNU Wget 1.24.5, a non-interactive network retriever.\n",
    "busybox": "BusyBox v1.36.1 (2024-06-10 07:11:47 UTC) multi-call binary.\n\n"
    "Usage: wget [-cqS] [--spider] [-O FILE] [-o LOGFILE] [--header STR]\n",
}


def main():
    flavor, log = sys.argv[1], sys.argv[2]
    args = sys.argv[3:]
    with open(log, "a") as f:
        f.write(" ".join(args) + "\n")
    if args == ["--help"]:
        sys.stderr.write(HELP[flavor])
        sys.exit(1 if flavor == "busybox" else 0)

    out, show, spider, hops, url = None, False, False, 20, None
    it = iter(args)
    for a in it:
        if a == "-O":
            out = next(it)
        elif a == "-S":
            show = True
        elif a == "-q":
            pass
        elif a == "--spider":
            spider = True
        elif flavor == "gnu" and a == "--https-only":
            pass
        elif flavor == "gnu" and a.startswith("--max-redirect="):
            hops = int(a.split("=", 1)[1])
        elif a.startswith("-"):
            sys.stderr.write("wget: unrecognized option '%s'\n" % a)
            sys.exit(1)
        else:
            url = a

    while True:
        u = urllib.parse.urlsplit(url)
        if u.scheme != "http":
            sys.exit(4)
        conn = http.client.HTTPConnection(u.hostname, u.port)
        conn.request("GET", u.path)
        r = conn.getresponse()
        if show:
            sys.stderr.write("  HTTP/1.1 %d %s\n" % (r.status, r.reason))
            for k, v in r.getheaders():
                sys.stderr.write("  %s: %s\n" % (k, v))
        if 300 <= r.status < 400:
            if hops == 0:
                sys.stderr.write("0 redirections exceeded.\n")
                sys.exit(8)
            hops -= 1
            url = urllib.parse.urljoin(url, r.getheader("Location"))
            continue
        if r.status >= 400:
            sys.exit(8)
        try:
            body = r.read()
        except http.client.IncompleteRead:
            sys.exit(4)
        if out and not spider:
            with open(out, "wb") as f:
                f.write(body)
        sys.exit(0)


if __name__ == "__main__":
    main()
