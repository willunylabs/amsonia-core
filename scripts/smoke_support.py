"""Interactive CLI helpers shared by the release smoke checks."""

import os
import pty
import select
import subprocess
import time


def enter(args, prompts, env):
    master, slave = pty.openpty()
    try:
        proc = subprocess.Popen(args, stdin=slave, stdout=slave, stderr=slave, env=env)
    except BaseException:
        os.close(master)
        raise
    finally:
        os.close(slave)
    try:
        pending = b""
        for prompt, value in prompts:
            deadline = time.monotonic() + 20
            while prompt not in pending:
                if time.monotonic() >= deadline or proc.poll() is not None:
                    raise RuntimeError(f"Command did not prompt for {prompt!r}")
                if select.select([master], [], [], 0.2)[0]:
                    pending += os.read(master, 4096)
            pending = pending.split(prompt, 1)[1]
            os.write(master, value.encode() + b"\n")
        if proc.wait(timeout=30):
            raise RuntimeError("Interactive operator command failed")
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()
        os.close(master)
