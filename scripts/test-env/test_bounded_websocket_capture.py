"""Adversarial transport tests, executed exclusively on test-env."""

import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import struct
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location('bounded_ws', Path(__file__).with_name('bounded_websocket_capture.py'))
WS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WS)


def frame(opcode, payload=b'', fin=True):
    first = opcode | (128 if fin else 0)
    length = len(payload)
    if length < 126:
        return bytes([first, length]) + payload
    if length < 65536:
        return bytes([first, 126]) + struct.pack('!H', length) + payload
    return bytes([first, 127]) + struct.pack('!Q', length) + payload


class Peer:
    def __init__(self, response_suffix=b'', accept=True, extra=b''):
        self.writes = []
        self.closed = False
        self.chunks = []
        self.suffix, self.accept, self.extra = response_suffix, accept, extra

    def settimeout(self, value):
        self.timeout = value

    def sendall(self, data):
        self.writes.append(data)
        if not data.startswith(b'GET '):
            return
        key = re.search(br'Sec-WebSocket-Key: ([^\r]+)', data).group(1)
        accept = base64.b64encode(hashlib.sha1(key + b'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest())
        if not self.accept:
            accept = b'invalid'
        response = (b'HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: keep-alive, Upgrade\r\n'
                    b'Sec-WebSocket-Accept: ' + accept + b'\r\n' + self.extra + b'\r\n' + self.suffix)
        self.chunks = [response[:13], response[13:61], response[61:]]

    def recv(self, _count):
        return self.chunks.pop(0) if self.chunks else b''

    def close(self):
        self.closed = True


class CaptureTests(unittest.TestCase):
    def capture(self):
        capture = WS.Capture('127.0.0.1', 18097, '/embywebsocket?api_key=synthetic-test-only')
        capture.sock, capture.open = Peer(), True
        return capture

    def feed(self, capture, raw):
        capture.buffer.extend(raw)
        capture._drain()

    def test_fragmented_utf8_and_interleaved_ping_preserve_one_message(self):
        capture = self.capture()
        raw = json.dumps({'MessageType': 'LibraryChanged', 'Data': {'name': '\u00e9'}}, ensure_ascii=False).encode()
        cut = raw.index(b'\xc3') + 1
        packet = frame(1, raw[:cut], False) + frame(9, b'alive') + frame(0, raw[cut:], True)
        for byte in packet:
            self.feed(capture, bytes([byte]))
        messages = [event['json'] for event in capture.record['events'] if 'json' in event]
        self.assertEqual(messages, [json.loads(raw)])
        self.assertIsNone(capture.fragment)
        pong = capture.sock.writes[0]
        self.assertEqual(pong[0], 138)
        self.assertTrue(pong[1] & 128)
        mask = pong[2:6]
        self.assertEqual(bytes(value ^ mask[i % 4] for i, value in enumerate(pong[6:])), b'alive')

    def test_single_text_and_binary_frames_remain_distinct(self):
        capture = self.capture()
        self.feed(capture, frame(1, b'{"MessageType":"LibraryChanged"}') + frame(2, b'\xff'))
        events = capture.record['events']
        self.assertEqual(events[0]['json']['MessageType'], 'LibraryChanged')
        self.assertNotIn('json', events[1])
        self.assertEqual(events[1]['payloadBase64'], '/w==')

    def test_reserved_masked_and_orphaned_continuations_fail(self):
        for raw in (b'\xc1\x00', b'\x81\x80', b'\x83\x00', frame(0), frame(9, b'', False), frame(8, b'x')):
            with self.subTest(raw=raw):
                with self.assertRaises(WS.ProtocolError):
                    self.feed(self.capture(), raw)

    def test_message_budget_includes_all_fragments(self):
        capture = self.capture()
        capture.MAX_MESSAGE = 8
        self.feed(capture, frame(1, b'12345', False))
        with self.assertRaises(WS.ProtocolError):
            self.feed(capture, frame(0, b'6789'))

    def test_declared_oversize_and_nonminimal_lengths_fail_before_body(self):
        for raw in (b'\x81\x7e\x00\x01', b'\x81\x7f' + struct.pack('!Q', 1),
                    b'\x81\x7f' + struct.pack('!Q', WS.Capture.MAX_MESSAGE + 1)):
            with self.subTest(raw=raw):
                with self.assertRaises(WS.ProtocolError):
                    self.feed(self.capture(), raw)

    def test_capture_and_event_budgets_bound_many_small_messages(self):
        capture = self.capture()
        capture.MAX_TOTAL = 3
        self.feed(capture, frame(1, b'abc'))
        with self.assertRaises(WS.ProtocolError):
            self.feed(capture, frame(1, b'd'))
        capture = self.capture()
        capture.MAX_EVENTS = 1
        self.feed(capture, frame(1, b''))
        with self.assertRaises(WS.ProtocolError):
            self.feed(capture, frame(1, b''))

    def test_interrupted_fragment_and_invalid_utf8_fail(self):
        capture = self.capture()
        self.feed(capture, frame(1, b'a', False))
        with self.assertRaises(WS.ProtocolError):
            self.feed(capture, frame(1, b'b'))
        capture = self.capture()
        with self.assertRaises(UnicodeDecodeError):
            self.feed(capture, frame(1, b'\xff'))
        self.assertEqual(capture.record['events'][-1]['payloadBase64'], '/w==')

    def test_invalid_close_status_is_retained_without_echo(self):
        for code in (999, 1004, 1005, 1006, 1015, 2000, 5000):
            with self.subTest(code=code):
                capture = self.capture()
                with self.assertRaises(WS.ProtocolError):
                    self.feed(capture, frame(8, struct.pack('!H', code)))
                self.assertEqual(capture.record['events'][-1]['closeCode'], code)
                self.assertEqual(capture.sock.writes, [])
                self.assertNotIn('serverCloseReceived', capture.record)

    def test_drain_stops_at_shared_observation_deadline(self):
        capture = self.capture()
        capture.buffer.extend(frame(9, b'a') + frame(9, b'b'))
        with patch.object(WS.time, 'monotonic', side_effect=[0, 0.1, 0.2, 0.3, 2]):
            capture._drain(1)
        self.assertEqual(len(capture.sock.writes), 1)
        self.assertEqual(bytes(capture.buffer), frame(9, b'b'))

    def test_segmented_handshake_keeps_first_message_and_rejects_replay(self):
        peer = Peer(frame(1, b'{"MessageType":"LibraryChanged"}'))
        capture = WS.Capture('127.0.0.1', 18097, '/embywebsocket?api_key=synthetic-test-only')
        with patch.object(WS.socket, 'create_connection', return_value=peer):
            capture.connect()
        self.assertTrue(capture.record['response']['acceptMatchesRequestKey'])
        self.assertEqual(capture.record['events'][0]['json']['MessageType'], 'LibraryChanged')
        with self.assertRaises(ValueError):
            capture.connect()
        self.feed(capture, frame(8, struct.pack('!H', 1000)))
        capture.close()
        self.assertTrue(peer.closed)
        self.assertTrue(capture.record['serverCloseReceived'])

    def test_bad_handshake_closes_connection(self):
        for peer in (Peer(accept=False), Peer(extra=b'Sec-WebSocket-Extensions: permessage-deflate\r\n'),
                     Peer(extra=b'Sec-WebSocket-Protocol: unrequested\r\n')):
            with self.subTest(peer=peer):
                capture = WS.Capture('127.0.0.1', 18097, '/embywebsocket?api_key=synthetic-test-only')
                with patch.object(WS.socket, 'create_connection', return_value=peer):
                    with self.assertRaises(WS.ProtocolError):
                        capture.connect()
                self.assertTrue(peer.closed)
                self.assertFalse(capture.open)

    def test_handshake_deadline_is_not_reset_by_slow_chunks(self):
        peer = Peer()
        capture = WS.Capture('127.0.0.1', 18097, '/embywebsocket?api_key=synthetic-test-only')
        clock = iter([0, 1, 2, 8, 11])
        with patch.object(WS.socket, 'create_connection', return_value=peer), patch.object(WS.time, 'monotonic', side_effect=lambda: next(clock)):
            with self.assertRaises(TimeoutError):
                capture.connect()
        self.assertTrue(peer.closed)

    def test_eof_records_incomplete_message_and_local_close(self):
        capture = self.capture()
        self.feed(capture, frame(1, b'partial', False))
        with patch.object(WS.select, 'select', return_value=([capture.sock], [], [])):
            capture.wait(0.1)
        capture.close()
        self.assertTrue(capture.record['peerEOF'])
        self.assertEqual(capture.record['incompleteMessageBytes'], 7)
        self.assertTrue(capture.record['transportClosed'])

    def test_endpoint_and_request_target_are_constrained(self):
        for host, port, path in (('remote.example', 18097, '/embywebsocket?x=1'),
                                 ('127.0.0.1', 80, '/embywebsocket?x=1'),
                                 ('127.0.0.1', 18097, '/embywebsocket?x=1\r\nInjected: yes')):
            with self.subTest(host=host, port=port, path=path):
                with self.assertRaises(ValueError):
                    WS.Capture(host, port, path)


if __name__ == '__main__':
    unittest.main()
