"""Spawn the reFlutter-patched app under the PairIP bypass and keep it running.

The app's TLS validation is already disabled statically by reFlutter, so with the
PairIP license check neutralised here the resigned app runs and its 5GenCare
control traffic flows to the mitmproxy listener (set as the device HTTP proxy).

Usage:
  python scripts/arm/spawn_capture.py [package] <agent.js>
Keeps running; Ctrl-C to detach. Script messages are printed with a tag.
"""

import sys
import time
import frida

PKG = sys.argv[1] if len(sys.argv) > 1 else "com.fivegencare.com.motorola.nursery"
# The compiled agent bundles frida-java-bridge (Frida 17 removed the built-in
# Java global). Its build location differs per machine, so it is an argument
# rather than a hard-coded path.
if len(sys.argv) > 2:
    SCRIPTS = [sys.argv[2]]
else:
    sys.exit(
        "usage: spawn_capture.py [package] <agent.js>\n"
        "       agent.js is the compiled frida agent (see fridaproj/)"
    )


def on_message(message, data):
    if message.get("type") == "send":
        print("MSG:", message.get("payload"))
    elif message.get("type") == "error":
        print("ERR:", message.get("stack") or message.get("description"))


def main():
    device = frida.get_usb_device(timeout=10)
    print("device:", device)
    pid = device.spawn([PKG])
    print("spawned pid:", pid)
    session = device.attach(pid)
    for path in SCRIPTS:
        with open(path, "r", encoding="utf-8") as f:
            src = f.read()
        script = session.create_script(src)
        script.on("message", on_message)
        script.load()
        print("loaded:", path)
    device.resume(pid)
    print("resumed; app running. Log in now. Ctrl-C to stop.")
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        print("detaching")


if __name__ == "__main__":
    main()
