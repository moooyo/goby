"""Bounded RFC 6455 observation transport; records are private until sanitized.

This module neither authenticates users nor changes server state. In particular,
an HTTP 101 response does not establish that an application token is authorized.
"""

import base64
import datetime
import hashlib
import json
import secrets
import select
import socket
import struct
import time


class ProtocolError(RuntimeError):
    """The peer exceeded the declared transport contract."""


class Capture:
    MAX_HEADER = 16384
    MAX_MESSAGE = 1 << 20
    MAX_TOTAL = 8 << 20
    MAX_EVENTS = 4096

    def __init__(self, host, port, path):
        if host != '127.0.0.1' or port != 18097:
            raise ValueError('Only the isolated reference loopback endpoint is allowed.')
        if not path.startswith('/embywebsocket?') or any(ord(c) < 33 or ord(c) > 126 for c in path):
            raise ValueError('The WebSocket request target is invalid.')
        self.host, self.port, self.path = host, port, path
        self.started = time.monotonic()
        self.sock = None
        self.open = False
        self.closed = False
        self.buffer = bytearray()
        self.fragment = None
        self.fragment_opcode = None
        self.total = 0
        self.record = {'request': {}, 'response': {}, 'events': [], 'annotations': [],
                       'observation': 'Times are client receipt/send times; queued frames may precede receipt. '
                                      'No compression or subprotocol is requested. HTTP 101 is not authentication proof.'}

    def stamp(self):
        return {'at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'elapsedMs': round((time.monotonic() - self.started) * 1000, 3)}

    def mark(self, label):
        if not isinstance(label, str) or not 1 <= len(label) <= 128:
            raise ValueError('An annotation needs a bounded label.')
        if len(self.record['annotations']) >= 256:
            raise ProtocolError('The annotation budget is exhausted.')
        self.record['annotations'].append(dict(self.stamp(), label=label))

    def append(self, value):
        if len(self.record['events']) >= self.MAX_EVENTS:
            raise ProtocolError('The event budget is exhausted.')
        self.record['events'].append(dict(self.stamp(), **value))

    def connect(self):
        if self.sock is not None or self.closed:
            raise ValueError('A capture connection cannot be replayed.')
        deadline = time.monotonic() + 10
        key = base64.b64encode(secrets.token_bytes(16)).decode('ascii')
        request = (f'GET {self.path} HTTP/1.1\r\nHost: {self.host}:{self.port}\r\n'
                   'Upgrade: websocket\r\nConnection: Upgrade\r\n'
                   f'Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n')
        self.record['request'] = {'method': 'GET', 'path': self.path, 'wireHeaders': request}
        try:
            self.sock = socket.create_connection((self.host, self.port), timeout=10)
            self.sock.settimeout(max(0.001, deadline - time.monotonic()))
            self.sock.sendall(request.encode('ascii'))
            while b'\r\n\r\n' not in self.buffer:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError('The handshake deadline expired.')
                self.sock.settimeout(remaining)
                chunk = self.sock.recv(4096)
                if not chunk:
                    raise ProtocolError('The peer closed during the handshake.')
                self.buffer.extend(chunk)
                end = self.buffer.find(b'\r\n\r\n')
                if (end < 0 and len(self.buffer) > self.MAX_HEADER) or end > self.MAX_HEADER:
                    raise ProtocolError('The handshake header is too large.')
            head, rest = bytes(self.buffer).split(b'\r\n\r\n', 1)
            self.buffer = bytearray(rest)
            lines = head.decode('iso-8859-1').split('\r\n')
            headers = {}
            for line in lines[1:]:
                if ':' not in line or line.startswith((' ', '\t')):
                    raise ProtocolError('A handshake header is malformed.')
                name, value = line.split(':', 1)
                headers.setdefault(name.lower(), []).append(value.strip())
            self.record['response'] = {'statusLine': lines[0], 'headers': headers,
                                       'wireHeaders': head.decode('iso-8859-1') + '\r\n\r\n'}
            if len(lines[0].split()) < 2 or lines[0].split()[:2] != ['HTTP/1.1', '101']:
                raise ProtocolError('The reference did not accept the WebSocket upgrade.')
            self.record['response']['status'] = 101
            expected = base64.b64encode(hashlib.sha1(
                (key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode('ascii')).digest()).decode('ascii')
            if headers.get('sec-websocket-accept') != [expected]:
                raise ProtocolError('The handshake accept value is invalid.')
            if [v.lower() for v in headers.get('upgrade', [])] != ['websocket']:
                raise ProtocolError('The upgrade response is invalid.')
            if 'upgrade' not in ','.join(headers.get('connection', [])).lower().replace(' ', '').split(','):
                raise ProtocolError('The connection response is invalid.')
            if 'sec-websocket-extensions' in headers or 'sec-websocket-protocol' in headers:
                raise ProtocolError('The peer selected an unrequested extension or protocol.')
            self.record['response']['acceptMatchesRequestKey'] = True
            self.sock.settimeout(2)
            self.open = True
            self._drain(deadline)
        except Exception as error:
            self.record['connectFailureType'] = type(error).__name__
            if self.sock is not None:
                self.sock.close()
            self.open = False
            self.closed = True
            self.record['transportClosed'] = True
            raise

    def _payload(self, opcode, payload):
        value = {'payloadBase64': base64.b64encode(payload).decode('ascii')}
        if opcode == 1:
            try:
                value['text'] = payload.decode('utf-8')
            except UnicodeDecodeError:
                return value
        return value

    def _send_control(self, opcode, payload, deadline=None):
        if not self.open or opcode not in (8, 10) or len(payload) > 125:
            raise ProtocolError('An invalid control response was requested.')
        mask = secrets.token_bytes(4)
        packet = bytes([0x80 | opcode, 0x80 | len(payload)]) + mask
        packet += bytes(value ^ mask[index % 4] for index, value in enumerate(payload))
        remaining = 2 if deadline is None else deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError('The control response deadline expired.')
        self.sock.settimeout(min(2, remaining))
        self.sock.sendall(packet)
        self.append(dict(direction='client-to-server', opcode=opcode, fin=True, masked=True,
                         payloadLength=len(payload), **self._payload(opcode, payload)))

    def _message(self, opcode, payload, frame):
        value = frame if frame is not None else dict(kind='message', direction='server-to-client',
                                                    opcode=opcode, payloadLength=len(payload),
                                                    **self._payload(opcode, payload))
        self.append(value)
        value = self.record['events'][-1]
        if opcode == 1:
            value['text'] = payload.decode('utf-8')
            try:
                value['json'] = json.loads(value['text'])
            except (ValueError, RecursionError):
                pass

    def _drain(self, deadline=None):
        deadline = time.monotonic() + 2 if deadline is None else deadline
        while self.open and len(self.buffer) >= 2:
            if time.monotonic() >= deadline:
                return
            first, second = self.buffer[:2]
            opcode, fin = first & 15, bool(first & 128)
            if first & 112 or second & 128 or opcode not in (0, 1, 2, 8, 9, 10):
                raise ProtocolError('The peer sent a reserved, masked, or unsupported frame.')
            length, offset = second & 127, 2
            if length == 126:
                if len(self.buffer) < 4:
                    return
                length, offset = struct.unpack('!H', self.buffer[2:4])[0], 4
                if length < 126:
                    raise ProtocolError('The frame length is not minimally encoded.')
            elif length == 127:
                if len(self.buffer) < 10:
                    return
                length, offset = struct.unpack('!Q', self.buffer[2:10])[0], 10
                if length < 65536:
                    raise ProtocolError('The frame length is not minimally encoded.')
            if length > self.MAX_MESSAGE or self.total + length > self.MAX_TOTAL:
                raise ProtocolError('The frame or capture payload budget is exhausted.')
            if opcode >= 8 and (not fin or length > 125 or (opcode == 8 and length == 1)):
                raise ProtocolError('The peer sent an invalid control frame.')
            if len(self.buffer) < offset + length:
                return
            payload = bytes(self.buffer[offset:offset + length])
            del self.buffer[:offset + length]
            self.total += length
            frame = dict(direction='server-to-client', opcode=opcode, fin=fin, masked=False,
                         payloadLength=length, **self._payload(opcode, payload))
            if opcode >= 8:
                if opcode == 8:
                    self.append(frame)
                    frame = self.record['events'][-1]
                    if payload:
                        frame['closeCode'] = struct.unpack('!H', payload[:2])[0]
                        frame['closeReason'] = payload[2:].decode('utf-8')
                        code = frame['closeCode']
                        if not (code in (1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011, 1012, 1013, 1014)
                                or 3000 <= code <= 4999):
                            raise ProtocolError('The peer sent an invalid close status.')
                    if not self.record.get('clientCloseSent'):
                        self._send_control(8, payload, deadline)
                        self.record['clientCloseSent'] = True
                    self.record['serverCloseReceived'] = True
                    self.open = False
                else:
                    self.append(frame)
                    if opcode == 9:
                        self._send_control(10, payload, deadline)
                continue
            if opcode == 0:
                if self.fragment is None:
                    raise ProtocolError('A continuation has no initial frame.')
                if len(self.fragment) + length > self.MAX_MESSAGE:
                    raise ProtocolError('The fragmented message is too large.')
                self.fragment.extend(payload)
                self.append(frame)
                if fin:
                    self._message(self.fragment_opcode, bytes(self.fragment), None)
                    self.fragment = self.fragment_opcode = None
            else:
                if self.fragment is not None:
                    raise ProtocolError('A second message interrupted a fragmented message.')
                if fin:
                    self._message(opcode, payload, frame)
                else:
                    self.fragment, self.fragment_opcode = bytearray(payload), opcode
                    self.append(frame)

    def wait(self, seconds):
        if not 0 <= seconds <= 10:
            raise ValueError('Each observation slice must be between zero and ten seconds.')
        deadline = time.monotonic() + seconds
        while self.open:
            self._drain(deadline)
            remaining = deadline - time.monotonic()
            if not self.open or remaining <= 0 or not select.select([self.sock], [], [], remaining)[0]:
                break
            chunk = self.sock.recv(65536)
            if not chunk:
                self.append({'direction': 'server-to-client', 'transport': 'eof'})
                self.record['peerEOF'] = True
                self.open = False
                break
            self.buffer.extend(chunk)
        self._drain(deadline)

    def close(self):
        if self.closed:
            return
        try:
            if self.open:
                self._send_control(8, struct.pack('!H', 1000))
                self.record['clientCloseSent'] = True
                self.wait(0.5)
        finally:
            self.record['observedDurationMs'] = self.stamp()['elapsedMs']
            self.record['unparsedBufferedBytes'] = len(self.buffer)
            self.record['incompleteMessageBytes'] = len(self.fragment) if self.fragment is not None else 0
            if self.sock is not None:
                self.sock.close()
            self.open = False
            self.closed = True
            self.record['transportClosed'] = True
