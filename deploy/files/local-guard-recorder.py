#!/usr/bin/env python3
"""Stands in for curing-outputd on the local bench (deploy/local-rig.sh).

There are no relay lines here. It records every connection and every request made to
the output guard's socket, and refuses each one, so the log shows whether anything
asked for a relay line to move. On the rig that request is the only way one moves.
"""
import os, socket, sys, threading, datetime
path, log = sys.argv[1], sys.argv[2]
def note(msg):
    with open(log, "a") as f:
        f.write(datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%fZ") + " " + msg + "\n")
def serve(c, n):
    note("client %d connected" % n)
    buf = b""
    try:
        while True:
            d = c.recv(4096)
            if not d:
                break
            buf += d
            while b"\n" in buf:
                line, buf = buf.split(b"\n", 1)
                note("client %d asked: %s" % (n, line.decode(errors="replace")))
                c.sendall(b"err recorder: there are no relay lines on this bench\n")
    finally:
        note("client %d closed" % n)
        c.close()
log = os.path.abspath(log)
os.chdir(os.path.dirname(os.path.abspath(path)))  # socket paths are limited to 108 bytes
name = os.path.basename(path)
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.bind(name); os.chmod(name, 0o666); s.listen(8)
note("recorder listening at /run/curing-outputs/guard.sock")
n = 0
while True:
    c, _ = s.accept(); n += 1
    threading.Thread(target=serve, args=(c, n), daemon=True).start()
