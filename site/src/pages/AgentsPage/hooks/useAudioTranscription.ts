import { useCallback, useRef, useState } from "react";

/**
 * Replaces useSpeechRecognition (browser Web Speech API) with a
 * record-then-transcribe flow: MediaRecorder captures audio locally, then
 * the recorded blob is POSTed to coderd's own /api/v2/audio-transcriptions
 * endpoint (server-side proxy to a configured OpenAI-Whisper-compatible
 * upstream, e.g. OmniRoute) once recording stops. This trades Web Speech's
 * live word-by-word interim results for materially better transcription
 * quality (a real STT model instead of the browser's on-device/cloud
 * recognizer) — `transcript` only ever changes ONCE per recording, after
 * the server round trip completes, not incrementally while speaking.
 *
 * Interface is intentionally close to the old hook's
 * (isSupported/isRecording/transcript/error/start/stop/cancel) so
 * AgentChatInput.tsx's own state machine barely changes — the one addition
 * is `isTranscribing`, the wait between "stop pressed" and "transcript
 * received" that Web Speech never had (it had no such gap: interim results
 * were already live).
 */

const AUDIO_TRANSCRIPTION_ENDPOINT = "/api/v2/audio-transcriptions";

/** Preference order: opus in a webm container is what Chrome/Firefox/Edge
 *  produce and what most STT providers (including Whisper) parse natively.
 *  Falling through to an empty string lets MediaRecorder pick its own
 *  platform default (e.g. Safari's audio/mp4) when none of these match. */
const PREFERRED_MIME_TYPES = [
	"audio/webm;codecs=opus",
	"audio/webm",
	"audio/ogg;codecs=opus",
];

function pickSupportedMimeType(): string {
	if (typeof MediaRecorder === "undefined") return "";
	for (const mimeType of PREFERRED_MIME_TYPES) {
		if (MediaRecorder.isTypeSupported(mimeType)) return mimeType;
	}
	return "";
}

export function isAudioTranscriptionSupported(): boolean {
	return (
		typeof MediaRecorder !== "undefined" &&
		typeof navigator !== "undefined" &&
		Boolean(navigator.mediaDevices?.getUserMedia)
	);
}

export type AudioTranscriptionError = "not-allowed" | "transcription-failed";

export function useAudioTranscription(): {
	isSupported: boolean;
	isRecording: boolean;
	isTranscribing: boolean;
	transcript: string;
	error: AudioTranscriptionError | null;
	start: () => void;
	stop: () => void;
	cancel: () => void;
} {
	const [isRecording, setIsRecording] = useState(false);
	const [isTranscribing, setIsTranscribing] = useState(false);
	const [transcript, setTranscript] = useState("");
	const [error, setError] = useState<AudioTranscriptionError | null>(null);

	const [isSupportedSnapshot] = useState(isAudioTranscriptionSupported);

	const mediaRecorderRef = useRef<MediaRecorder | null>(null);
	const streamRef = useRef<MediaStream | null>(null);
	const chunksRef = useRef<BlobPart[]>([]);
	const cancelledRef = useRef(false);

	const releaseStream = useCallback(() => {
		streamRef.current?.getTracks().forEach((track) => track.stop());
		streamRef.current = null;
	}, []);

	const start = useCallback(() => {
		if (!isSupportedSnapshot) return;

		setError(null);
		setTranscript("");
		cancelledRef.current = false;
		chunksRef.current = [];

		navigator.mediaDevices
			.getUserMedia({ audio: true })
			.then((stream) => {
				if (cancelledRef.current) {
					stream.getTracks().forEach((track) => track.stop());
					return;
				}
				streamRef.current = stream;

				const mimeType = pickSupportedMimeType();
				const recorder = mimeType
					? new MediaRecorder(stream, { mimeType })
					: new MediaRecorder(stream);

				recorder.ondataavailable = (event) => {
					if (event.data.size > 0) chunksRef.current.push(event.data);
				};

				recorder.onstop = () => {
					releaseStream();
					if (cancelledRef.current) return;

					const blob = new Blob(chunksRef.current, {
						type: recorder.mimeType || mimeType || "audio/webm",
					});
					chunksRef.current = [];

					setIsTranscribing(true);
					fetch(AUDIO_TRANSCRIPTION_ENDPOINT, {
						method: "POST",
						headers: { "Content-Type": blob.type },
						body: blob,
					})
						.then(async (res) => {
							if (!res.ok) throw new Error(`transcription failed: ${res.status}`);
							const data = (await res.json()) as { text?: string };
							if (cancelledRef.current) return;
							setTranscript(data.text ?? "");
						})
						.catch(() => {
							if (cancelledRef.current) return;
							setError("transcription-failed");
						})
						.finally(() => {
							if (!cancelledRef.current) setIsTranscribing(false);
						});
				};

				mediaRecorderRef.current = recorder;
				setIsRecording(true);
				recorder.start();
			})
			.catch(() => {
				setError("not-allowed");
				setIsRecording(false);
			});
	}, [isSupportedSnapshot, releaseStream]);

	// Stop recording and let the onstop handler above transcribe what was
	// captured — the normal "accept" path.
	const stop = useCallback(() => {
		setIsRecording(false);
		const recorder = mediaRecorderRef.current;
		mediaRecorderRef.current = null;
		if (recorder && recorder.state !== "inactive") {
			recorder.stop();
		} else {
			releaseStream();
		}
	}, [releaseStream]);

	// Discard the in-progress (or just-finished) recording entirely — no
	// transcription request is ever sent for a cancelled recording.
	const cancel = useCallback(() => {
		cancelledRef.current = true;
		const recorder = mediaRecorderRef.current;
		mediaRecorderRef.current = null;
		if (recorder && recorder.state !== "inactive") {
			recorder.stop();
		}
		releaseStream();
		chunksRef.current = [];
		setIsRecording(false);
		setIsTranscribing(false);
		setTranscript("");
		setError(null);
	}, [releaseStream]);

	return {
		isSupported: isSupportedSnapshot,
		isRecording,
		isTranscribing,
		transcript,
		error,
		start,
		stop,
		cancel,
	};
}
