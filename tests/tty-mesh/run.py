#!/usr/bin/env python3
# SPDX-License-Identifier: MPL-2.0
"""Run concurrent Lua/PTY agents on a fully connected 2..8-node mesh."""
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
    parser.add_argument("--ssh", help="SSH target for node b; other nodes run locally")
    parser.add_argument("--peer-address", help="mesh IP of SSH host")
    parser.add_argument("--local-address", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=19470)
    parser.add_argument("--nodes", type=int, default=2, choices=range(2, 9))
    parser.add_argument("--commands", type=int, default=20)
    args = parser.parse_args()
    if args.commands < 1:
        parser.error("commands must be positive")
    if args.ssh and (not args.peer_address or args.local_address == "127.0.0.1"):
        parser.error("SSH mode requires --peer-address and a reachable --local-address")
    binary = str(Path(args.binary).resolve())
    nodes = [chr(ord('a') + i) for i in range(args.nodes)]
    peers = {node: {"Address": args.peer_address if args.ssh and node == 'b' else args.local_address,
                    "Port": args.port + i} for i, node in enumerate(nodes)}
    processes, logs = [], []
    remote_dir = None
    ssh_options = ["-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=2", "-o", "ServerAliveCountMax=2"]

    def run(command, **kwargs):
        kwargs.setdefault("timeout", 15)
        kwargs.setdefault("stdin", subprocess.DEVNULL)
        kwargs.setdefault("check", True)
        return subprocess.run(command, **kwargs)

    def ssh(command, **kwargs):
        return run(["ssh", "-n", *ssh_options, args.ssh, shlex.join(command)], **kwargs)

    def copy(*paths):
        run(["scp", "-q", *ssh_options, *paths], timeout=30)

    def reports():
        result = {}
        for node, log in zip(nodes, logs):
            log.seek(0)
            for line in log.read().splitlines():
                if line.startswith("{"):
                    report = json.loads(line)
                    if report.get("result") == "PASS":
                        result[node] = report
        return result

    with tempfile.TemporaryDirectory(prefix="tty-mesh-proof-") as directory:
        scratch = Path(directory)
        run([binary, "-init", directory, "-nodes", str(args.nodes), "-ips",
             ",".join(sorted({"127.0.0.1", *(peer["Address"] for peer in peers.values())}))])
        (scratch / "peers.json").write_text(json.dumps(peers))
        try:
            if args.ssh:
                remote_dir = ssh(["mktemp", "-d", "/tmp/tty-mesh-proof-XXXXXXXX"], capture_output=True, text=True).stdout.strip()
                if not remote_dir.startswith("/tmp/tty-mesh-proof-") or "/" in remote_dir[len("/tmp/"):]:
                    remote_dir = None
                    raise RuntimeError("unexpected remote scratch path")
                copy(binary, *(str(scratch / name) for name in ("keys.json", "cert.pem", "key.pem", "peers.json")), f"{args.ssh}:{remote_dir}/")
            for i, node in enumerate(nodes):
                remote = bool(args.ssh and node == "b")
                root = remote_dir if remote else directory
                command = [f"{root}/{Path(binary).name}" if remote else binary,
                           "-keys", root, "-node", node, "-bind", peers[node]["Address"], "-port", str(peers[node]["Port"]),
                           "-peer-node", nodes[(i + 1) % len(nodes)], "-recipient-node", nodes[(i - 1) % len(nodes)],
                           "-mesh-peers", root + "/peers.json", "-commands", str(args.commands),
                           "-refs-out", f"{root}/{node}.json", "-peer-refs", f"{root}/{nodes[(i + 1) % len(nodes)]}.json",
                           "-release-file", root + "/release"]
                if remote:
                    launch = f"echo $$ > {shlex.quote(root + '/pid')}; exec {shlex.join(command)}"
                    command = ["ssh", "-n", *ssh_options, args.ssh, launch]
                log = open(scratch / f"{node}.log", "w+")
                logs.append(log)
                processes.append(subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT))
            deadline = time.monotonic() + 100
            exchanged, released = not args.ssh, False
            while time.monotonic() < deadline and any(p.poll() is None for p in processes):
                if any(p.poll() not in (None, 0) for p in processes):
                    break
                if not exchanged:
                    next_node = nodes[2 % len(nodes)]
                    if (scratch / f"{next_node}.json").exists() and ssh(["test", "-s", remote_dir + "/b.json"], check=False).returncode == 0:
                        copy(str(scratch / f"{next_node}.json"), f"{args.ssh}:{remote_dir}/{next_node}.json")
                        copy(f"{args.ssh}:{remote_dir}/b.json", str(scratch / "b.json"))
                        exchanged = True
                if not released and len(reports()) == args.nodes:
                    (scratch / "release").touch()
                    if args.ssh:
                        ssh(["touch", remote_dir + "/release"])
                    released = True
                time.sleep(.05)
            result = reports()
            for node, log in zip(nodes, logs):
                log.seek(0)
                print(f"node {node}:\n{log.read()}", flush=True)
            if (len(result) != args.nodes or not all(p.poll() == 0 for p in processes)
                    or any(report.get("connected_peers") != args.nodes - 1 for report in result.values())):
                raise RuntimeError("multi-node Lua/PTY proof failed")
        finally:
            # Kill the owned remote node before terminating its bootstrap SSH.
            try:
                if args.ssh and remote_dir:
                    command = f"if test -f {shlex.quote(remote_dir + '/pid')}; then kill $(cat {shlex.quote(remote_dir + '/pid')}) 2>/dev/null || true; fi"
                    ssh(["sh", "-c", command], check=False)
            finally:
                for process in processes:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
                for log in logs:
                    log.close()
                if args.ssh and remote_dir:
                    ssh(["rm", "-rf", "--", remote_dir])


if __name__ == "__main__":
    main()
