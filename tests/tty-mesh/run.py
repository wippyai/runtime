#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Run the symmetric Lua/PTY proof locally or over an authorized SSH host."""
import argparse
import json
from pathlib import Path
import shlex
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="/tmp/wippy-tty-mesh-proof")
    parser.add_argument("--ssh", help="SSH target for node b")
    parser.add_argument("--peer-address", help="mesh IP of SSH host")
    parser.add_argument("--local-address", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=19470)
    args = parser.parse_args()
    if args.ssh and (not args.peer_address or args.local_address == "127.0.0.1"):
        parser.error("SSH mode requires --peer-address and a reachable --local-address")
    binary = str(Path(args.binary).resolve())
    peer_address = args.peer_address or "127.0.0.1"
    processes = []
    remote_dir = None

    def ssh(command, **kwargs):
        return subprocess.run(["ssh", "-o", "BatchMode=yes", args.ssh, shlex.join(command)], check=True, **kwargs)

    with tempfile.TemporaryDirectory(prefix="tty-mesh-proof-") as directory:
        scratch = Path(directory)
        subprocess.run([binary, "-init", directory, "-ips", f"127.0.0.1,{args.local_address},{peer_address}"], check=True)
        try:
            if args.ssh:
                remote_dir = ssh(["mktemp", "-d", "/tmp/tty-mesh-proof-XXXXXXXX"], capture_output=True, text=True).stdout.strip()
                if not remote_dir.startswith("/tmp/tty-mesh-proof-") or "/" in remote_dir[len("/tmp/"):]:
                    raise RuntimeError("unexpected remote scratch path")
                subprocess.run(["scp", "-q", binary, str(scratch / "keys.json"), str(scratch / "cert.pem"), str(scratch / "key.pem"), f"{args.ssh}:{remote_dir}/"], check=True)
            else:
                remote_dir = directory
            a_command = [binary, "-keys", directory, "-node", "a", "-bind", args.local_address,
                         "-port", str(args.port), "-peer-address", peer_address, "-peer-port", str(args.port + 1),
                         "-refs-out", str(scratch / "a.json"), "-peer-refs", str(scratch / "b.json")]
            b_command = [f"{remote_dir}/{Path(binary).name}" if args.ssh else binary,
                         "-keys", remote_dir, "-node", "b", "-bind", peer_address,
                         "-port", str(args.port + 1), "-peer-address", args.local_address, "-peer-port", str(args.port),
                         "-refs-out", f"{remote_dir}/b.json", "-peer-refs", f"{remote_dir}/a.json"]
            if args.ssh:
                # Save the process ID so failed proofs can terminate only their
                # own remote process, without touching the host's other work.
                command = f"echo $$ > {shlex.quote(remote_dir + '/pid')}; exec {shlex.join(b_command)}"
                b_command = ["ssh", "-o", "BatchMode=yes", args.ssh, command]
            logs = [open(scratch / f"{name}.log", "w+") for name in ("a", "b")]
            for command, log in zip((a_command, b_command), logs):
                processes.append(subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT))
            deadline = time.monotonic() + 100
            exchanged = not args.ssh
            while time.monotonic() < deadline and any(p.poll() is None for p in processes):
                if any(p.poll() not in (None, 0) for p in processes):
                    break
                if not exchanged and (scratch / "a.json").exists():
                    exists = subprocess.run(["ssh", "-o", "BatchMode=yes", args.ssh,
                                             shlex.join(["test", "-s", remote_dir + "/b.json"])], capture_output=True).returncode == 0
                    if exists:
                        subprocess.run(["scp", "-q", str(scratch / "a.json"), f"{args.ssh}:{remote_dir}/a.json"], check=True)
                        subprocess.run(["scp", "-q", f"{args.ssh}:{remote_dir}/b.json", str(scratch / "b.json")], check=True)
                        exchanged = True
                time.sleep(0.1)
            success = all(p.poll() == 0 for p in processes)
            for name, log in zip(("a", "b"), logs):
                log.flush()
                log.seek(0)
                output = log.read()
                print(f"node {name}:\n{output}", flush=True)
                if not any(line.startswith("{") and json.loads(line).get("result") == "PASS" for line in output.splitlines()):
                    success = False
            if not success:
                raise RuntimeError("two-node Lua/PTY proof failed")
        finally:
            for process in processes:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
            if args.ssh and remote_dir:
                # The private keys and bound test references never leave the
                # disposable directories and are removed after the proof.
                command = f"if test -f {shlex.quote(remote_dir + '/pid')}; then kill $(cat {shlex.quote(remote_dir + '/pid')}) 2>/dev/null || true; fi"
                subprocess.run(["ssh", "-o", "BatchMode=yes", args.ssh, command], check=False)
                ssh(["rm", "-rf", "--", remote_dir])


if __name__ == "__main__":
    main()
