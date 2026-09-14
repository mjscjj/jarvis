#!/usr/bin/env python3
"""Host-owned egress for the development container. Never exposes a host API.

Credentials stay on the host. The CLI bridge permits identity checks, directory
lookups, and notification-bot operations only; no personal message/file access.
"""
import argparse
import http.server
import ipaddress
import re
import json
import os
import select
import socket
import socketserver
import subprocess

MAIN = 'directory'
BOT = 'notification'
HOSTS = {
    'chatgpt.com', 'auth.openai.com', 'api.openai.com',
    'open.feishu.cn', 'accounts.feishu.cn', 'passport.feishu.cn',
    'cloud.byteintl.net', 'registry.npmjs.org', 'proxy.golang.org',
    'sum.golang.org', 'storage.googleapis.com', 'bnpm.byted.org',
}


def command(args):
    """Reconstruct supported commands; never accept arbitrary global options."""
    args = list(args)
    profile = MAIN
    if args[:1] == ['--profile'] and len(args) >= 2:
        profile, args = args[1], args[2:]
    if profile not in {MAIN, BOT}:
        raise ValueError('only directory and notification profiles are available')
    if args == ['profile', 'list']:
        return ['profile', 'list']
    if args == ['auth', 'status', '--verify']:
        return ['--profile', profile, *args]
    if args[-2:] == ['--format', 'json']:
        args = args[:-2]
    flags = {}
    if profile == MAIN and args[:2] == ['contact', '+search-user']:
        rest = args[2:]
        allowed = {'--query', '--user-ids', '--as'}
        prefix = ['--profile', MAIN, 'contact', '+search-user']
    elif profile == MAIN and args[:3] == ['api', 'GET', '/open-apis/search/v1/user']:
        rest, allowed = args[3:], {'--params', '--as'}
        prefix = ['--profile', MAIN, *args[:3]]
    elif profile == BOT and args[:1] == ['api'] and len(args) >= 3:
        method, path = args[1:3]
        if (method, path) not in {
            ('GET', '/open-apis/application/v6/scopes'),
            ('GET', '/open-apis/contact/v3/scopes'),
            ('GET', '/open-apis/contact/v3/users/find_by_department'),
            ('GET', '/open-apis/contact/v3/users/batch'),
            ('GET', '/open-apis/application/v2/app/visibility'),
            ('POST', '/open-apis/im/v1/messages'),
        } and not (method == 'GET' and re.fullmatch(r'/open-apis/contact/v3/departments/[A-Za-z0-9_-]+/children', path)):
            raise ValueError('notification API denied')
        rest, allowed = args[3:], {'--params', '--data', '--as'}
        prefix = ['--profile', BOT, 'api', method, path]
    elif profile == BOT and args[:2] == ['im', '+messages-mget']:
        rest, allowed = args[2:], {'--message-ids', '--as'}
        prefix = ['--profile', BOT, 'im', '+messages-mget']
    else:
        raise ValueError('personal messages, tasks and arbitrary CLI commands are unavailable')
    if len(rest) % 2:
        raise ValueError('expected flag/value pairs')
    for flag, value in zip(rest[::2], rest[1::2]):
        if flag not in allowed or flag in flags or not isinstance(value, str) or value.startswith(('@', '-')):
            raise ValueError('unsupported CLI option')
        if flag in {'--data', '--params'}:
            json.loads(value)
        flags[flag] = value
    expected = 'user' if profile == MAIN else 'bot'
    if flags.get('--as') != expected:
        raise ValueError('wrong identity')
    return prefix + [v for pair in flags.items() for v in pair] + ['--format', 'json']


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass  # Never log authorization payloads or login URLs.

    def do_CONNECT(self):
        self.close_connection = True
        if self.path not in {host + ':443' for host in HOSTS}:
            self.send_error(403, 'egress target denied')
            return
        host = self.path[:-4]
        try:
            addresses = socket.getaddrinfo(host, 443, type=socket.SOCK_STREAM)
            if not addresses or any(ipaddress.ip_address(a[4][0]).is_loopback or (not ipaddress.ip_address(a[4][0]).is_global and host != 'bnpm.byted.org') for a in addresses):
                self.send_error(403, 'non-public address denied')
                return
            remote = socket.create_connection(addresses[0][4][:2], timeout=15)
        except OSError:
            self.send_error(502, 'egress connection failed')
            return
        with remote:
            self.send_response(200, 'Connection Established')
            self.end_headers()
            self.wfile.flush()
            self.connection.setblocking(False)
            remote.setblocking(False)
            try:
                while True:
                    readable, _, _ = select.select([self.connection, remote], [], [], 120)
                    if not readable:
                        return
                    for source in readable:
                        data = source.recv(65536)
                        if not data:
                            return
                        dest = remote if source is self.connection else self.connection
                        dest.setblocking(True)
                        dest.sendall(data)
                        dest.setblocking(False)
            except OSError:
                return
        self.close_connection = True

    def do_POST(self):
        if self.path != '/lark':
            self.send_error(403)
            return
        try:
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 < size <= 1048576:
                raise ValueError('invalid request length')
            data = json.loads(self.rfile.read(size))
            args = command(data['args'])
            result = subprocess.run([self.server.cli, *args], input=data.get('stdin', ''),
                                    text=True, capture_output=True, timeout=65)
            stdout = result.stdout
            if args == ['profile', 'list'] and result.returncode == 0:
                stdout = json.dumps([p for p in json.loads(stdout) if p['name'] in {MAIN, BOT}])
            payload = {'stdout': stdout, 'stderr': result.stderr, 'exit_code': result.returncode}
        except (ValueError, KeyError, TypeError):
            self.send_error(403, 'CLI operation denied')
            return
        except subprocess.TimeoutExpired:
            self.send_error(504, 'CLI timeout')
            return
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--socket', required=True)
    p.add_argument('--cli', required=True)
    p.add_argument('--directory-profile', required=True)
    p.add_argument('--notification-profile', required=True)
    opts = p.parse_args()
    MAIN, BOT = opts.directory_profile, opts.notification_profile
    os.umask(0o077)
    if os.path.exists(opts.socket):
        os.unlink(opts.socket)
    with Server(opts.socket, Handler) as server:
        server.cli = opts.cli
        server.serve_forever()
