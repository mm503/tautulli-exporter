#!/usr/bin/env python3
import requests
import time
from prometheus_client import start_http_server, Gauge

TAUTULLI_URL = "http://url_here"
API_KEY = "api_key_here"

# Stream metrics
active_streams_total = Gauge('plex_active_streams_total', 'Total number of active Plex streams')
active_streams_direct = Gauge('plex_active_streams_direct', 'Number of direct play streams')
active_streams_transcode = Gauge('plex_active_streams_transcode', 'Number of transcoding streams')
active_users = Gauge('plex_active_users', 'Number of active Plex users')

# Transcoding details
transcode_video = Gauge('plex_transcode_video_sessions', 'Video transcoding sessions')
transcode_audio = Gauge('plex_transcode_audio_sessions', 'Audio transcoding sessions')
transcode_container = Gauge('plex_transcode_container_sessions', 'Container transcoding sessions')

def get_tautulli_activity():
    url = f"{TAUTULLI_URL}/api/v2"
    params = {
        'apikey': API_KEY,
        'cmd': 'get_activity'
    }

    try:
        response = requests.get(url, params=params, timeout=10)
        data = response.json()

        if data['response']['result'] == 'success':
            sessions = data['response']['data']['sessions']

            # Count totals
            total_streams = len(sessions)
            direct_streams = 0
            transcode_streams = 0
            video_transcodes = 0
            audio_transcodes = 0
            container_transcodes = 0

            # Analyze each session
            for session in sessions:
                # Check transcoding status
                video_decision = session.get('transcode_video_decision', 'direct play')
                audio_decision = session.get('transcode_audio_decision', 'direct play')
                container_decision = session.get('transcode_container_decision', 'direct play')

                # Count transcode types
                if video_decision == 'transcode':
                    video_transcodes += 1
                if audio_decision == 'transcode':
                    audio_transcodes += 1
                if container_decision == 'transcode':
                    container_transcodes += 1

                # Overall stream classification
                if (video_decision == 'transcode' or
                    audio_decision == 'transcode' or
                    container_decision == 'transcode'):
                    transcode_streams += 1
                else:
                    direct_streams += 1

            # Update metrics
            active_streams_total.set(total_streams)
            active_streams_direct.set(direct_streams)
            active_streams_transcode.set(transcode_streams)
            active_users.set(len(set(session['user'] for session in sessions)))

            transcode_video.set(video_transcodes)
            transcode_audio.set(audio_transcodes)
            transcode_container.set(container_transcodes)

            print(f"Total: {total_streams}, Direct: {direct_streams}, Transcode: {transcode_streams}")

    except Exception as e:
        print(f"Error fetching Tautulli data: {e}")

if __name__ == '__main__':
    start_http_server(8000)
    while True:
        get_tautulli_activity()
        time.sleep(30)
