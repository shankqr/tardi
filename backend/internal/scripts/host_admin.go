package scripts

// HostAdminInstallScript installs the root-owned host helper that OpenClaw can
// call through a Unix socket mounted into the gateway container. It exposes
// desktop lifecycle actions plus a generic host.exec root bridge used by the
// in-container sudo shim.
const HostAdminInstallScript = `#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

if ! command -v python3 >/dev/null 2>&1; then
    apt-get update -qq
    apt-get install -y -qq python3
fi

INSTALL_DIR=/usr/local/lib/tardi-host-admin
BIN_DIR=/opt/openclaw/host-admin/bin
RUN_DIR=/run/tardi-host-admin
LOG_DIR=/var/log/tardi-host-admin

mkdir -p "$INSTALL_DIR" "$BIN_DIR" "$RUN_DIR" "$LOG_DIR"
chown root:root "$INSTALL_DIR" "$LOG_DIR"
chmod 755 "$INSTALL_DIR" "$LOG_DIR"

if id openclaw >/dev/null 2>&1; then
    chown root:openclaw "$RUN_DIR"
    chmod 0770 "$RUN_DIR"
else
    chmod 0700 "$RUN_DIR"
fi

cat > "$INSTALL_DIR/server.py" <<'PYEOF'
#!/usr/bin/env python3
import datetime
import fcntl
import grp
import http.server
import json
import os
import pwd
import re
import shlex
import shutil
import socketserver
import subprocess
import sys
import urllib.parse

SOCKET_DIR = "/run/tardi-host-admin"
SOCKET_PATH = SOCKET_DIR + "/admin.sock"
LOCK_PATH = SOCKET_DIR + "/action.lock"
LOG_PATH = "/var/log/tardi-host-admin/actions.log"
MAX_BODY = 65536
MAX_EXEC_SECONDS = 3600


class ActionError(Exception):
    def __init__(self, message, status=400, output=""):
        super().__init__(message)
        self.status = status
        self.output = output


def openclaw_ids():
    try:
        user = pwd.getpwnam("openclaw")
        group = grp.getgrnam("openclaw")
        return user.pw_uid, group.gr_gid
    except KeyError:
        return 1000, 1000


def log_action(action, ok, detail=""):
    ts = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
    line = json.dumps({"ts": ts, "action": action, "ok": ok, "detail": detail[-1000:]}, sort_keys=True)
    with open(LOG_PATH, "a", encoding="utf-8") as f:
        f.write(line + "\n")


def run(args, timeout=120):
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    output = proc.stdout or ""
    if proc.returncode != 0:
        raise ActionError("command failed", 500, output[-6000:])
    return output[-6000:]


def run_bash(script, timeout=120):
    return run(["/bin/bash", "-lc", script], timeout=timeout)


def body_timeout(body, default=300):
    try:
        timeout = int(body.get("timeout") or default)
    except (TypeError, ValueError):
        raise ActionError("invalid timeout", 400)
    if timeout < 1:
        raise ActionError("timeout must be positive", 400)
    return min(timeout, MAX_EXEC_SECONDS)


def service_state(name):
    proc = subprocess.run(["systemctl", "is-active", name], text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    active = (proc.stdout or "").strip()
    proc = subprocess.run(["systemctl", "is-enabled", name], text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    enabled = (proc.stdout or "").strip()
    return {"active": active or "unknown", "enabled": enabled or "unknown"}


def desktop_status(_body):
    return {
        "desktop_service": service_state("tardi-desktop.service"),
        "host_admin_service": service_state("tardi-host-admin.service"),
        "commands": {
            "vncserver": shutil.which("vncserver") or "",
            "startxfce4": shutil.which("startxfce4") or "",
            "tradingview": shutil.which("tradingview") or "",
            "xdotool": shutil.which("xdotool") or "",
            "wmctrl": shutil.which("wmctrl") or "",
        },
        "socket": SOCKET_PATH,
        "display": ":1",
    }


def desktop_install(_body):
    script = r'''
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

apt-get update -qq
apt-get install -y -qq \
    ca-certificates curl dbus-x11 fonts-dejavu gnupg jq \
    tigervnc-common tigervnc-standalone-server \
    wmctrl x11-apps x11-utils x11-xserver-utils xdotool \
    xfce4 xfce4-terminal

install -m 0755 -d /usr/share/keyrings
curl -fsSL https://tvd-packages.tradingview.com/keyring.gpg \
    -o /usr/share/keyrings/tradingview-desktop-archive-keyring.gpg
printf '%s\n' 'deb [arch=amd64 signed-by=/usr/share/keyrings/tradingview-desktop-archive-keyring.gpg] https://tvd-packages.tradingview.com/ubuntu/stable jammy multiverse' \
    > /etc/apt/sources.list.d/tradingview-desktop.list
apt-get update -qq
apt-get install -y -qq tradingview

install -o openclaw -g openclaw -m 700 -d /home/openclaw/.vnc
install -o openclaw -g openclaw -m 700 -d /tmp/tardi-runtime-openclaw
if [ ! -s /home/openclaw/.vnc/passwd ]; then
    python3 -c 'import secrets; print(secrets.token_urlsafe(24))' | vncpasswd -f > /home/openclaw/.vnc/passwd
    chown openclaw:openclaw /home/openclaw/.vnc/passwd
    chmod 600 /home/openclaw/.vnc/passwd
fi

cat > /home/openclaw/.vnc/xstartup <<'VNCXSTARTUP'
#!/bin/sh
unset SESSION_MANAGER
unset DBUS_SESSION_BUS_ADDRESS
[ -r "$HOME/.Xresources" ] && xrdb "$HOME/.Xresources"
startxfce4 &
VNCXSTARTUP
chown openclaw:openclaw /home/openclaw/.vnc/xstartup
chmod 755 /home/openclaw/.vnc/xstartup

cat > /etc/systemd/system/tardi-desktop.service <<'SVCEOF'
[Unit]
Description=Tardi private XFCE VNC desktop
After=network.target

[Service]
Type=forking
User=openclaw
PAMName=login
WorkingDirectory=/home/openclaw
Environment=USER=openclaw
Environment=HOME=/home/openclaw
Environment=DISPLAY=:1
Environment=XDG_RUNTIME_DIR=/tmp/tardi-runtime-openclaw
ExecStartPre=-/usr/bin/vncserver -kill :1
ExecStart=/usr/bin/vncserver :1 -localhost yes -geometry 1440x900 -depth 24
ExecStop=/usr/bin/vncserver -kill :1
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable tardi-desktop.service
'''
    output = run_bash(script, timeout=1800)
    return {"message": "desktop profile installed", "output": output}


def desktop_start(_body):
    run(["systemctl", "start", "tardi-desktop.service"], timeout=90)
    return {"message": "desktop started", "desktop_service": service_state("tardi-desktop.service")}


def desktop_stop(_body):
    run(["systemctl", "stop", "tardi-desktop.service"], timeout=90)
    return {"message": "desktop stopped", "desktop_service": service_state("tardi-desktop.service")}


def desktop_restart(_body):
    run(["systemctl", "restart", "tardi-desktop.service"], timeout=120)
    return {"message": "desktop restarted", "desktop_service": service_state("tardi-desktop.service")}


def desktop_open(body):
    symbol = str(body.get("symbol") or "BINANCE:BTCUSDT")
    if not re.match(r"^[A-Za-z0-9:_./-]{1,80}$", symbol):
        raise ActionError("invalid symbol", 400)
    url = "https://www.tradingview.com/chart/?symbol=" + urllib.parse.quote(symbol, safe="")
    launch = (
        "export DISPLAY=:1; "
        "export XDG_RUNTIME_DIR=/tmp/tardi-runtime-openclaw; "
        "if ! command -v tradingview >/dev/null 2>&1; then echo tradingview command not found >&2; exit 127; fi; "
        "nohup tradingview --no-sandbox " + shlex.quote(url) + " >/home/openclaw/.tardi-tradingview.log 2>&1 &"
    )
    script = (
        "set -euo pipefail\n"
        "install -o openclaw -g openclaw -m 700 -d /tmp/tardi-runtime-openclaw\n"
        "systemctl start tardi-desktop.service\n"
        "for i in $(seq 1 30); do [ -S /tmp/.X11-unix/X1 ] && break; sleep 1; done\n"
        "runuser -u openclaw -- bash -lc " + shlex.quote(launch) + "\n"
    )
    output = run_bash(script, timeout=120)
    return {"message": "TradingView launch requested", "symbol": symbol, "url": url, "output": output}


def host_exec(body):
    cmd = body.get("cmd")
    if not isinstance(cmd, str) or not cmd.strip():
        raise ActionError("cmd is required", 400)
    if len(cmd.encode("utf-8")) > 60000:
        raise ActionError("cmd is too large", 400)
    timeout = body_timeout(body)
    output = run_bash(cmd, timeout=timeout)
    return {"message": "host command completed", "timeout": timeout, "output": output}


ACTIONS = {
    "desktop.status": (desktop_status, False),
    "desktop.install": (desktop_install, True),
    "desktop.start": (desktop_start, True),
    "desktop.stop": (desktop_stop, True),
    "desktop.restart": (desktop_restart, True),
    "desktop.open": (desktop_open, True),
    "host.exec": (host_exec, True),
}


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "TardiHostAdmin/1.0"

    def log_message(self, fmt, *args):
        return

    def write_json(self, status, payload):
        data = json.dumps(payload, sort_keys=True).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/health":
            self.write_json(200, {"ok": True})
            return
        self.write_json(404, {"ok": False, "error": "not_found"})

    def do_POST(self):
        if self.path != "/v1/run":
            self.write_json(404, {"ok": False, "error": "not_found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length < 1 or length > MAX_BODY:
                raise ActionError("invalid body size", 400)
            body = json.loads(self.rfile.read(length).decode("utf-8"))
            action = str(body.get("action") or "")
            if action not in ACTIONS:
                raise ActionError("unknown action", 400)
            handler, needs_lock = ACTIONS[action]
            if needs_lock:
                with open(LOCK_PATH, "w", encoding="utf-8") as lock:
                    fcntl.flock(lock, fcntl.LOCK_EX)
                    result = handler(body)
            else:
                result = handler(body)
            log_action(action, True)
            self.write_json(200, {"ok": True, "action": action, "result": result})
        except ActionError as exc:
            log_action(locals().get("action", ""), False, exc.output or str(exc))
            self.write_json(exc.status, {"ok": False, "error": str(exc), "output": exc.output})
        except subprocess.TimeoutExpired as exc:
            output = (exc.stdout or "") if isinstance(exc.stdout, str) else ""
            log_action(locals().get("action", ""), False, "timeout " + output)
            self.write_json(504, {"ok": False, "error": "action timed out", "output": output[-6000:]})
        except Exception as exc:
            log_action(locals().get("action", ""), False, str(exc))
            self.write_json(500, {"ok": False, "error": "internal_error", "detail": str(exc)})


class UnixHTTPServer(socketserver.UnixStreamServer):
    allow_reuse_address = True


def main():
    os.makedirs(SOCKET_DIR, exist_ok=True)
    os.makedirs(os.path.dirname(LOG_PATH), exist_ok=True)
    uid, gid = openclaw_ids()
    os.chown(SOCKET_DIR, 0, gid)
    os.chmod(SOCKET_DIR, 0o770)
    if os.path.exists(SOCKET_PATH):
        os.unlink(SOCKET_PATH)
    server = UnixHTTPServer(SOCKET_PATH, Handler)
    os.chown(SOCKET_PATH, 0, gid)
    os.chmod(SOCKET_PATH, 0o660)
    try:
        server.serve_forever()
    finally:
        server.server_close()
        try:
            os.unlink(SOCKET_PATH)
        except FileNotFoundError:
            pass


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(0)
PYEOF
chmod 755 "$INSTALL_DIR/server.py"

cat > "$BIN_DIR/tardi-host-admin" <<'CLIENTEOF'
#!/bin/sh
set -eu

ACTION="${1:-}"
SOCKET="${TARDI_HOST_ADMIN_SOCKET:-/run/tardi-host-admin/admin.sock}"

if [ -z "$ACTION" ]; then
    echo "usage: tardi-host-admin <desktop.status|desktop.install|desktop.start|desktop.stop|desktop.restart|desktop.open|host.exec> [args...]" >&2
    exit 64
fi
shift || true

json_string() {
    printf '"%s"' "$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g')"
}

ACTION_JSON=$(json_string "$ACTION")
case "$ACTION" in
    desktop.open)
        SYMBOL="${1:-}"
        SYMBOL_JSON=$(json_string "$SYMBOL")
        BODY="{\"action\":${ACTION_JSON},\"symbol\":${SYMBOL_JSON}}"
        ;;
    host.exec)
        TIMEOUT="${TARDI_HOST_EXEC_TIMEOUT:-300}"
        if [ "${1:-}" = "--timeout" ]; then
            if [ "$#" -lt 3 ]; then
                echo "usage: tardi-host-admin host.exec [--timeout seconds] <command>" >&2
                exit 64
            fi
            TIMEOUT="$2"
            shift 2
        fi
        case "$TIMEOUT" in
            ''|*[!0-9]*)
                echo "host.exec timeout must be an integer number of seconds" >&2
                exit 64
                ;;
        esac
        CMD="$*"
        if [ -z "$CMD" ]; then
            echo "usage: tardi-host-admin host.exec [--timeout seconds] <command>" >&2
            exit 64
        fi
        CMD_JSON=$(json_string "$CMD")
        BODY="{\"action\":${ACTION_JSON},\"cmd\":${CMD_JSON},\"timeout\":${TIMEOUT}}"
        ;;
    *)
        BODY="{\"action\":${ACTION_JSON}}"
        ;;
esac

TMP_BODY=$(mktemp)
STATUS=$(curl --unix-socket "$SOCKET" -sS \
    -o "$TMP_BODY" \
    -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    --data-binary "$BODY" \
    http://tardi-host-admin/v1/run) || {
    RC=$?
    rm -f "$TMP_BODY"
    exit "$RC"
}
cat "$TMP_BODY"
echo
rm -f "$TMP_BODY"
case "$STATUS" in
    2*) exit 0 ;;
    *) exit 1 ;;
esac
CLIENTEOF
chmod 755 "$BIN_DIR/tardi-host-admin"

cat > "$BIN_DIR/sudo" <<'SUDOEOF'
#!/bin/sh
set -eu

while [ "$#" -gt 0 ]; do
    case "$1" in
        -n|-S|-E|-H|-k|-K|-v)
            shift
            ;;
        --)
            shift
            break
            ;;
        *)
            break
            ;;
    esac
done

if [ "$#" -eq 0 ]; then
    echo "usage: sudo <command...>" >&2
    exit 64
fi

quote_sh() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

CMD=""
for ARG in "$@"; do
    if [ -z "$CMD" ]; then
        CMD=$(quote_sh "$ARG")
    else
        CMD="$CMD $(quote_sh "$ARG")"
    fi
done

exec /opt/tardi/bin/tardi-host-admin host.exec "$CMD"
SUDOEOF
chmod 755 "$BIN_DIR/sudo"
chown -R root:root /opt/openclaw/host-admin

cat > /etc/systemd/system/tardi-host-admin.service <<'SVCEOF'
[Unit]
Description=Tardi host admin action helper
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/python3 /usr/local/lib/tardi-host-admin/server.py
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable tardi-host-admin.service >/dev/null 2>&1 || true
systemctl restart tardi-host-admin.service
`
