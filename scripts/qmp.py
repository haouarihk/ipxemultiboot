#!/usr/bin/env python3
"""Minimal QMP client for driving the test VM.

  qmp.py SOCK shot out.png        screenshot
  qmp.py SOCK keys down down ret  press keys (QEMU qcode names)
  qmp.py SOCK type "text"         type text (US layout)
  qmp.py SOCK reset               reboot the VM
  qmp.py SOCK quit                stop the VM
"""
import json
import socket
import sys
import time

CHARS = {" ": "spc", "-": "minus", ".": "dot", "/": "slash", "=": "equal", "\n": "ret",
         ";": "semicolon", "'": "apostrophe", ",": "comma", "\\": "backslash", "`": "grave_accent",
         "[": "bracket_left", "]": "bracket_right", "\t": "tab"}
SHIFTED = {"|": "backslash", "*": "8", "&": "7", ">": "dot", "<": "comma", ":": "semicolon",
           '"': "apostrophe", "_": "minus", "+": "equal", "$": "4", "(": "9", ")": "0", "~": "grave_accent",
           "!": "1", "@": "2", "#": "3", "%": "5", "^": "6", "?": "slash", "{": "bracket_left", "}": "bracket_right"}


def keyseq(c):
    if c in SHIFTED:
        return ["shift", SHIFTED[c]]
    if c.isupper():
        return ["shift", c.lower()]
    return [CHARS.get(c, c)]


def main():
    sock_path, cmd, *args = sys.argv[1:]
    s = socket.socket(socket.AF_UNIX)
    s.connect(sock_path)
    f = s.makefile("rw")
    f.readline()  # greeting

    def call(execute, **arguments):
        f.write(json.dumps({"execute": execute, "arguments": arguments}) + "\n")
        f.flush()
        while True:
            msg = json.loads(f.readline())
            if "return" in msg or "error" in msg:
                if "error" in msg:
                    sys.exit(msg["error"]["desc"])
                return msg["return"]

    call("qmp_capabilities")
    if cmd == "shot":
        call("screendump", filename=args[0], format="png")
    elif cmd in ("keys", "type"):
        combos = [[k] for k in args] if cmd == "keys" else [keyseq(c) for c in args[0]]
        for combo in combos:
            call("send-key", keys=[{"type": "qcode", "data": k} for k in combo])
            time.sleep(0.08)
    elif cmd == "reset":
        call("system_reset")
    elif cmd == "quit":
        call("quit")


if __name__ == "__main__":
    main()
