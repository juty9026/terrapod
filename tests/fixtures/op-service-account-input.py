"""Drive public helper prompts on a real terminal, using dummy credentials only."""

import errno
import json
import os
import pty
import select
import signal
import sys
import termios
import time

home, bin_dir, command, steps_json = sys.argv[1:]
steps = json.loads(steps_json)
pid, terminal = pty.fork()
if pid == 0:
    os.execve("/bin/zsh", ["zsh", "-d", "-ic", command], {
        "HOME": home, "PATH": f"{bin_dir}:/usr/bin:/bin", "TERM": "dumb",
    })

output = bytearray()
hidden = []
deadline = time.monotonic() + 10
try:
    while time.monotonic() < deadline:
        if steps and steps[0][0].encode() in output:
            prompt, response, secret = steps.pop(0)
            if secret:
                # The prompt can arrive just before read -s disables echo.
                echo_deadline = time.monotonic() + 1
                while termios.tcgetattr(terminal)[3] & termios.ECHO:
                    if time.monotonic() >= echo_deadline:
                        break
                    time.sleep(0.01)
                hidden.append(not bool(termios.tcgetattr(terminal)[3] & termios.ECHO))
            os.write(terminal, response.encode())
        if select.select([terminal], [], [], 0.05)[0]:
            try:
                chunk = os.read(terminal, 65536)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                break
            if not chunk:
                break
            output.extend(chunk)
    else:
        raise TimeoutError("helper did not finish its terminal interaction")
    _, status = os.waitpid(pid, 0)
    pid = None
    print(json.dumps({
        "status": os.waitstatus_to_exitcode(status),
        "output": output.decode(errors="replace"),
        "hidden": hidden,
        "pending": len(steps),
    }))
finally:
    os.close(terminal)
    if pid is not None:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
