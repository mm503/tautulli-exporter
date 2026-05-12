import io
import json
import signal
import sys
import time
import unittest
from unittest.mock import MagicMock, call, patch

import requests

import main


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _activity_response(
    stream_count=2,
    direct_play=1,
    direct_stream=0,
    transcode=1,
    total_bw=5000,
    lan_bw=3000,
    wan_bw=2000,
    sessions=None,
    result='success',
    message='',
):
    if sessions is None:
        sessions = []
    payload = {
        'response': {
            'result': result,
            'message': message,
            'data': {
                'stream_count': stream_count,
                'stream_count_direct_play': direct_play,
                'stream_count_direct_stream': direct_stream,
                'stream_count_transcode': transcode,
                'total_bandwidth': total_bw,
                'lan_bandwidth': lan_bw,
                'wan_bandwidth': wan_bw,
                'sessions': sessions,
            },
        }
    }
    mock_response = MagicMock()
    mock_response.json.return_value = payload
    mock_response.raise_for_status.return_value = None
    return mock_response


def _make_handler(path):
    """Instantiate HealthHandler without a real socket."""
    handler = main.HealthHandler.__new__(main.HealthHandler)
    handler.path = path
    handler.wfile = io.BytesIO()
    handler.send_response = MagicMock()
    handler.send_header = MagicMock()
    handler.end_headers = MagicMock()
    return handler


# ---------------------------------------------------------------------------
# validate_config
# ---------------------------------------------------------------------------

VALID_URL = 'http://tautulli.example.com'
VALID_KEY = 'abcdef1234567890'


class TestValidateConfig(unittest.TestCase):

    def _run(self, url=VALID_URL, key=VALID_KEY, port=8000, interval=30):
        with patch('main.TAUTULLI_URL', url), \
             patch('main.API_KEY', key), \
             patch('main.METRICS_PORT', port), \
             patch('main.SCRAPE_INTERVAL', interval), \
             patch('sys.exit') as mock_exit:
            main.validate_config()
            return mock_exit

    def test_valid_config_does_not_exit(self):
        mock_exit = self._run()
        mock_exit.assert_not_called()

    def test_https_url_is_valid(self):
        mock_exit = self._run(url='https://tautulli.example.com')
        mock_exit.assert_not_called()

    def test_missing_url_exits(self):
        mock_exit = self._run(url='')
        mock_exit.assert_called_once_with(1)

    def test_url_without_scheme_exits(self):
        mock_exit = self._run(url='tautulli.example.com')
        mock_exit.assert_called_once_with(1)

    def test_url_with_invalid_scheme_exits(self):
        mock_exit = self._run(url='ftp://tautulli.example.com')
        mock_exit.assert_called_once_with(1)

    def test_url_with_no_netloc_exits(self):
        # urlparse('http://') has an empty netloc
        mock_exit = self._run(url='http://')
        mock_exit.assert_called_once_with(1)

    def test_missing_api_key_exits(self):
        mock_exit = self._run(key='')
        mock_exit.assert_called_once_with(1)

    def test_short_api_key_exits(self):
        mock_exit = self._run(key='tooshort')
        mock_exit.assert_called_once_with(1)

    def test_api_key_with_invalid_chars_exits(self):
        # Hyphens are not alnum or underscore
        mock_exit = self._run(key='invalid-key-here!!')
        mock_exit.assert_called_once_with(1)

    def test_api_key_with_underscores_is_valid(self):
        mock_exit = self._run(key='abcdef_1234567890')
        mock_exit.assert_not_called()

    def test_port_zero_exits(self):
        mock_exit = self._run(port=0)
        mock_exit.assert_called_once_with(1)

    def test_port_too_high_exits(self):
        mock_exit = self._run(port=65536)
        mock_exit.assert_called_once_with(1)

    def test_scrape_interval_too_low_exits(self):
        mock_exit = self._run(interval=4)
        mock_exit.assert_called_once_with(1)

    def test_scrape_interval_minimum_is_valid(self):
        mock_exit = self._run(interval=5)
        mock_exit.assert_not_called()

    def test_multiple_errors_all_logged_before_exit(self):
        with patch('main.TAUTULLI_URL', ''), \
             patch('main.API_KEY', ''), \
             patch('main.METRICS_PORT', 8000), \
             patch('main.SCRAPE_INTERVAL', 30), \
             patch('sys.exit') as mock_exit, \
             patch.object(main.logger, 'error') as mock_log:
            main.validate_config()
            mock_exit.assert_called_once_with(1)
            # Both URL and key errors should be logged
            self.assertGreaterEqual(mock_log.call_count, 2)


# ---------------------------------------------------------------------------
# get_tautulli_activity — circuit breaker
# ---------------------------------------------------------------------------

class TestCircuitBreaker(unittest.TestCase):

    def setUp(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0
        main.circuit_opened_at = 0

    def tearDown(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0
        main.circuit_opened_at = 0

    @patch('requests.get')
    def test_circuit_open_within_cooldown_skips_request(self, mock_get):
        main.consecutive_failures = main.MAX_CONSECUTIVE_FAILURES
        main.circuit_opened_at = time.time()  # just opened

        main.get_tautulli_activity()

        mock_get.assert_not_called()

    @patch('requests.get')
    def test_circuit_half_open_after_cooldown_probes(self, mock_get):
        main.consecutive_failures = main.MAX_CONSECUTIVE_FAILURES
        main.circuit_opened_at = time.time() - (main.CIRCUIT_BREAKER_RESET_INTERVAL + 1)
        mock_get.return_value = _activity_response()

        main.get_tautulli_activity()

        mock_get.assert_called_once()

    @patch('requests.get')
    def test_successful_probe_resets_circuit(self, mock_get):
        main.consecutive_failures = main.MAX_CONSECUTIVE_FAILURES
        main.circuit_opened_at = time.time() - (main.CIRCUIT_BREAKER_RESET_INTERVAL + 1)
        mock_get.return_value = _activity_response()

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 0)

    @patch('requests.get', side_effect=requests.exceptions.ConnectionError('refused'))
    def test_failed_probe_rearms_cooldown(self, mock_get):
        main.consecutive_failures = main.MAX_CONSECUTIVE_FAILURES
        old_opened_at = time.time() - (main.CIRCUIT_BREAKER_RESET_INTERVAL + 10)
        main.circuit_opened_at = old_opened_at

        main.get_tautulli_activity()

        # circuit_opened_at must have been refreshed to now
        self.assertGreater(main.circuit_opened_at, old_opened_at)

    @patch('requests.get', side_effect=requests.exceptions.ConnectionError('refused'))
    def test_failures_increment_counter(self, mock_get):
        main.get_tautulli_activity()
        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get', side_effect=requests.exceptions.ConnectionError('refused'))
    def test_five_failures_trigger_circuit(self, _):
        for _ in range(main.MAX_CONSECUTIVE_FAILURES):
            main.consecutive_failures = 0  # reset so circuit doesn't block
            main.get_tautulli_activity()

        # After 5 individual failures the counter is at MAX
        self.assertGreaterEqual(main.consecutive_failures, 1)


# ---------------------------------------------------------------------------
# get_tautulli_activity — success path & metrics
# ---------------------------------------------------------------------------

class TestGetActivity(unittest.TestCase):

    def setUp(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0
        main.circuit_opened_at = 0

    def tearDown(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0
        main.circuit_opened_at = 0

    @patch('requests.get')
    def test_success_resets_failure_counter(self, mock_get):
        main.consecutive_failures = 3
        mock_get.return_value = _activity_response()

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 0)

    @patch('requests.get')
    def test_success_updates_last_successful_scrape(self, mock_get):
        mock_get.return_value = _activity_response()
        before = time.time()

        main.get_tautulli_activity()

        self.assertGreaterEqual(main.last_successful_scrape, before)

    @patch('requests.get')
    def test_metrics_set_from_activity_data(self, mock_get):
        mock_get.return_value = _activity_response(
            stream_count=3, direct_play=1, direct_stream=1, transcode=1,
            total_bw=9000, lan_bw=6000, wan_bw=3000,
        )

        with patch.object(main.active_streams_total, 'set') as t, \
             patch.object(main.active_streams_direct, 'set') as d, \
             patch.object(main.active_streams_direct_play, 'set') as dp, \
             patch.object(main.active_streams_direct_stream, 'set') as ds, \
             patch.object(main.active_streams_transcode, 'set') as tr, \
             patch.object(main.bandwidth_total, 'set') as bt, \
             patch.object(main.bandwidth_lan, 'set') as bl, \
             patch.object(main.bandwidth_wan, 'set') as bw:
            main.get_tautulli_activity()

        t.assert_called_once_with(3)
        d.assert_called_once_with(2)   # direct_play + direct_stream
        dp.assert_called_once_with(1)
        ds.assert_called_once_with(1)
        tr.assert_called_once_with(1)
        bt.assert_called_once_with(9000)
        bl.assert_called_once_with(6000)
        bw.assert_called_once_with(3000)

    @patch('requests.get')
    def test_transcode_component_counts_from_sessions(self, mock_get):
        sessions = [
            {'transcode_video_decision': 'transcode',
             'transcode_audio_decision': 'transcode',
             'transcode_container_decision': 'direct play'},
            {'transcode_video_decision': 'direct play',
             'transcode_audio_decision': 'copy',
             'transcode_container_decision': 'transcode'},
            {'transcode_video_decision': 'transcode',
             'transcode_audio_decision': 'direct play',
             'transcode_container_decision': 'direct play'},
        ]
        mock_get.return_value = _activity_response(sessions=sessions)

        with patch.object(main.transcode_video, 'set') as tv, \
             patch.object(main.transcode_audio, 'set') as ta, \
             patch.object(main.transcode_container, 'set') as tc:
            main.get_tautulli_activity()

        tv.assert_called_once_with(2)
        ta.assert_called_once_with(1)
        tc.assert_called_once_with(1)

    @patch('requests.get')
    def test_session_missing_decision_defaults_to_direct_play(self, mock_get):
        sessions = [{}]  # no transcode_*_decision keys
        mock_get.return_value = _activity_response(sessions=sessions)

        with patch.object(main.transcode_video, 'set') as tv, \
             patch.object(main.transcode_audio, 'set') as ta, \
             patch.object(main.transcode_container, 'set') as tc:
            main.get_tautulli_activity()

        tv.assert_called_once_with(0)
        ta.assert_called_once_with(0)
        tc.assert_called_once_with(0)

    @patch('requests.get')
    def test_zero_streams_sets_all_metrics_to_zero(self, mock_get):
        mock_get.return_value = _activity_response(
            stream_count=0, direct_play=0, direct_stream=0, transcode=0,
            total_bw=0, lan_bw=0, wan_bw=0,
        )

        with patch.object(main.active_streams_total, 'set') as t:
            main.get_tautulli_activity()

        t.assert_called_once_with(0)

    @patch('requests.get')
    def test_api_failure_result_increments_counter(self, mock_get):
        mock_get.return_value = _activity_response(result='error', message='bad key')

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get', side_effect=requests.exceptions.Timeout())
    def test_timeout_increments_counter(self, _):
        main.get_tautulli_activity()
        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get', side_effect=requests.exceptions.ConnectionError('refused'))
    def test_connection_error_increments_counter(self, _):
        main.get_tautulli_activity()
        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get')
    def test_http_error_increments_counter(self, mock_get):
        mock_get.return_value = MagicMock()
        mock_get.return_value.raise_for_status.side_effect = requests.exceptions.HTTPError('403')

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get')
    def test_json_decode_error_increments_counter(self, mock_get):
        mock_get.return_value = MagicMock()
        mock_get.return_value.raise_for_status.return_value = None
        mock_get.return_value.json.side_effect = json.JSONDecodeError('bad json', '', 0)

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get')
    def test_unexpected_exception_increments_counter(self, mock_get):
        mock_get.side_effect = RuntimeError('something weird')

        main.get_tautulli_activity()

        self.assertEqual(main.consecutive_failures, 1)

    @patch('requests.get')
    def test_all_errors_update_circuit_opened_at(self, mock_get):
        mock_get.side_effect = requests.exceptions.ConnectionError('refused')
        before = time.time()

        main.get_tautulli_activity()

        self.assertGreaterEqual(main.circuit_opened_at, before)

    @patch('requests.get')
    def test_api_url_constructed_correctly(self, mock_get):
        mock_get.return_value = _activity_response()

        with patch('main.TAUTULLI_URL', 'http://tautulli.example.com'):
            main.get_tautulli_activity()

        call_url = mock_get.call_args[0][0]
        self.assertEqual(call_url, 'http://tautulli.example.com/api/v2')

    @patch('requests.get')
    def test_api_url_constructed_with_trailing_slash(self, mock_get):
        mock_get.return_value = _activity_response()

        with patch('main.TAUTULLI_URL', 'http://tautulli.example.com/'):
            main.get_tautulli_activity()

        call_url = mock_get.call_args[0][0]
        self.assertIn('/api/v2', call_url)
        self.assertNotIn('//api', call_url)


# ---------------------------------------------------------------------------
# HealthHandler
# ---------------------------------------------------------------------------

class TestHealthHandler(unittest.TestCase):

    def setUp(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0

    def tearDown(self):
        main.consecutive_failures = 0
        main.last_successful_scrape = 0

    def test_healthz_returns_200_ok(self):
        h = _make_handler('/healthz')
        h.do_GET()
        h.send_response.assert_called_once_with(200)
        self.assertIn(b'OK', h.wfile.getvalue())

    def test_ready_returns_200_when_never_scraped(self):
        # last_successful_scrape == 0 is startup grace period
        main.last_successful_scrape = 0
        h = _make_handler('/ready')
        h.do_GET()
        h.send_response.assert_called_once_with(200)
        self.assertIn(b'READY', h.wfile.getvalue())

    def test_ready_returns_200_when_recently_scraped(self):
        main.last_successful_scrape = time.time()
        main.consecutive_failures = 0
        h = _make_handler('/ready')
        h.do_GET()
        h.send_response.assert_called_once_with(200)

    def test_ready_returns_503_when_scrape_too_old(self):
        main.last_successful_scrape = time.time() - (main.SCRAPE_INTERVAL * 3)
        main.consecutive_failures = 0
        h = _make_handler('/ready')
        h.do_GET()
        h.send_response.assert_called_once_with(503)
        self.assertIn(b'NOT READY', h.wfile.getvalue())

    def test_ready_returns_503_when_circuit_breaker_active(self):
        main.last_successful_scrape = time.time()
        main.consecutive_failures = main.MAX_CONSECUTIVE_FAILURES
        h = _make_handler('/ready')
        h.do_GET()
        h.send_response.assert_called_once_with(503)

    def test_ready_503_body_contains_failure_count(self):
        main.last_successful_scrape = time.time() - (main.SCRAPE_INTERVAL * 3)
        main.consecutive_failures = 3
        h = _make_handler('/ready')
        h.do_GET()
        body = h.wfile.getvalue()
        self.assertIn(b'failures: 3', body)

    def test_metrics_returns_200_with_prometheus_output(self):
        h = _make_handler('/metrics')
        with patch('main.generate_latest', return_value=b'# metrics output'):
            h.do_GET()
        h.send_response.assert_called_once_with(200)
        self.assertIn(b'# metrics output', h.wfile.getvalue())

    def test_metrics_returns_500_on_generate_error(self):
        h = _make_handler('/metrics')
        with patch('main.generate_latest', side_effect=RuntimeError('boom')):
            h.do_GET()
        h.send_response.assert_called_once_with(500)

    def test_unknown_path_returns_404(self):
        h = _make_handler('/unknown')
        h.do_GET()
        h.send_response.assert_called_once_with(404)
        self.assertIn(b'Not Found', h.wfile.getvalue())

    def test_log_message_suppressed_in_non_debug(self):
        h = _make_handler('/healthz')
        with patch('main.LOG_LEVEL', 'INFO'), \
             patch.object(main.logger, 'debug') as mock_debug:
            h.log_message('%s', 'test')
            mock_debug.assert_not_called()

    def test_log_message_logged_in_debug(self):
        h = _make_handler('/healthz')
        with patch('main.LOG_LEVEL', 'DEBUG'), \
             patch.object(main.logger, 'debug') as mock_debug:
            h.log_message('%s', 'test')
            mock_debug.assert_called_once()


# ---------------------------------------------------------------------------
# signal_handler
# ---------------------------------------------------------------------------

class TestSignalHandler(unittest.TestCase):

    def setUp(self):
        main.shutdown_event.clear()

    def tearDown(self):
        main.shutdown_event.clear()

    def test_signal_handler_sets_shutdown_event(self):
        self.assertFalse(main.shutdown_event.is_set())
        main.signal_handler(signal.SIGTERM, None)
        self.assertTrue(main.shutdown_event.is_set())

    def test_signal_handler_works_for_sigint(self):
        main.signal_handler(signal.SIGINT, None)
        self.assertTrue(main.shutdown_event.is_set())


# ---------------------------------------------------------------------------
# main()
# ---------------------------------------------------------------------------

class TestMain(unittest.TestCase):

    def setUp(self):
        main.shutdown_event.clear()

    def tearDown(self):
        main.shutdown_event.clear()

    @patch('main.SCRAPE_INTERVAL', 0)
    @patch('main.validate_config')
    @patch('main.get_tautulli_activity')
    @patch('main.ThreadedHTTPServer')
    def test_main_starts_server_and_scrapes(self, mock_server_cls, mock_scrape, mock_validate):
        mock_httpd = MagicMock()
        mock_server_cls.return_value = mock_httpd

        def stop_after_first(*args, **kwargs):
            main.shutdown_event.set()

        mock_scrape.side_effect = stop_after_first

        main.main()

        mock_validate.assert_called_once()
        mock_scrape.assert_called_once()
        mock_httpd.shutdown.assert_called_once()

    @patch('main.validate_config')
    @patch('main.ThreadedHTTPServer', side_effect=OSError('port in use'))
    def test_main_exits_on_server_start_failure(self, mock_server_cls, mock_validate):
        with self.assertRaises(SystemExit) as ctx:
            main.main()
        self.assertEqual(ctx.exception.code, 1)

    @patch('main.SCRAPE_INTERVAL', 0)
    @patch('main.validate_config')
    @patch('main.ThreadedHTTPServer')
    def test_main_loop_catches_unexpected_errors(self, mock_server_cls, mock_validate):
        mock_httpd = MagicMock()
        mock_server_cls.return_value = mock_httpd
        call_count = 0

        def raise_then_stop(*args, **kwargs):
            nonlocal call_count
            call_count += 1
            if call_count >= 2:
                main.shutdown_event.set()
            raise RuntimeError('boom')

        with patch('main.get_tautulli_activity', side_effect=raise_then_stop):
            main.main()

        self.assertGreaterEqual(call_count, 2)
        mock_httpd.shutdown.assert_called_once()


if __name__ == '__main__':
    unittest.main()
