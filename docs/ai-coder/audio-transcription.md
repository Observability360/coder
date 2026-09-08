# Audio transcription (voice input for AI agents)

The AI agent chat's microphone button records audio locally in the browser
and sends it to coderd's own `POST /api/v2/audio-transcriptions` endpoint,
which proxies it to a single configured OpenAI-Whisper-compatible upstream
(for example, an [OmniRoute](https://github.com/Observability360/OmniRoute)
deployment's `/v1/audio/transcriptions`). The upstream API key is held
server-side only — it is never sent to the browser.

This replaces the previous browser Web Speech API implementation, which
depended entirely on the browser's own on-device/cloud recognizer for
transcription quality (no control over model choice, no vocabulary hints).

## Configuration

Transcription is disabled (the endpoint returns `404`) unless both the URL
and API key are set.

| Flag | Env var | Description |
| --- | --- | --- |
| `--transcription-url` | `CODER_TRANSCRIPTION_URL` | OpenAI-Whisper-compatible URL to proxy requests to. |
| `--transcription-api-key` | `CODER_TRANSCRIPTION_API_KEY` | Bearer token sent to the URL above. Secret — held server-side only. |
| `--transcription-model` | `CODER_TRANSCRIPTION_MODEL` | Model id sent to the upstream (default `whisper-1`). |
| `--transcription-language` | `CODER_TRANSCRIPTION_LANGUAGE` | ISO-639-1 language hint (e.g. `pt` for Portuguese). Empty lets the provider auto-detect. |
| `--transcription-timeout` | `CODER_TRANSCRIPTION_TIMEOUT` | Maximum time to wait for a response (default `30s`). |

## Client flow

1. Press the microphone button — the browser starts a `MediaRecorder`
   capturing microphone audio (typically `audio/webm;codecs=opus`).
2. Press again (now showing an accept icon) to stop — the recorded blob is
   uploaded to `/api/v2/audio-transcriptions`.
3. While the upload/transcription round trip is in flight, the input shows
   "Transcrevendo..." and the send button is disabled.
4. On success, the transcript is inserted into the prompt — the user can
   still edit it before sending. On failure, an explicit error is shown;
   there is no silent fallback to browser speech recognition.

Canceling a recording (the same button, before pressing accept) discards it
locally — no transcription request is ever sent.
