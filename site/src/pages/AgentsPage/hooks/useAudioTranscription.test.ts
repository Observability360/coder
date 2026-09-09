import { act, renderHook, waitFor } from "@testing-library/react";
import { API } from "#/api/api";
import {
	isAudioTranscriptionSupported,
	useAudioTranscription,
} from "./useAudioTranscription";

// ---------------------------------------------------------------------------
// Minimal mock for the MediaRecorder + getUserMedia browser APIs.
// ---------------------------------------------------------------------------

class MockMediaRecorder {
	static supportedMimeType = "audio/webm;codecs=opus";
	static isTypeSupported = vi.fn(
		(mimeType: string) => mimeType === MockMediaRecorder.supportedMimeType,
	);

	mimeType: string;
	state: "inactive" | "recording" = "recording";
	ondataavailable: ((event: { data: Blob }) => void) | null = null;
	onstop: (() => void) | null = null;

	constructor(
		public stream: MediaStream,
		options?: { mimeType?: string },
	) {
		this.mimeType = options?.mimeType ?? "";
	}

	start = vi.fn();
	stop = vi.fn(() => {
		this.state = "inactive";
		this.ondataavailable?.({ data: new Blob(["fake-audio"]) });
		this.onstop?.();
	});
}

let lastRecorder: MockMediaRecorder | null = null;
let mockTracks: { stop: ReturnType<typeof vi.fn> }[] = [];

function installMocks() {
	lastRecorder = null;
	mockTracks = [{ stop: vi.fn() }];

	class Ctor extends MockMediaRecorder {
		constructor(stream: MediaStream, options?: { mimeType?: string }) {
			super(stream, options);
			lastRecorder = this;
		}
	}
	Object.assign(window, { MediaRecorder: Ctor });

	Object.assign(navigator, {
		mediaDevices: {
			getUserMedia: vi.fn().mockResolvedValue({
				getTracks: () => mockTracks,
			}),
		},
	});
}

function removeMediaRecorder() {
	Object.assign(window, { MediaRecorder: undefined });
}

function denyMicAccess() {
	Object.assign(navigator, {
		mediaDevices: {
			getUserMedia: vi.fn().mockRejectedValue(new Error("Permission denied")),
		},
	});
}

afterEach(() => {
	vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------

describe("isAudioTranscriptionSupported", () => {
	it("is false when MediaRecorder is unavailable", () => {
		removeMediaRecorder();
		expect(isAudioTranscriptionSupported()).toBe(false);
	});

	it("is true when MediaRecorder and getUserMedia are both available", () => {
		installMocks();
		expect(isAudioTranscriptionSupported()).toBe(true);
	});
});

describe("useAudioTranscription", () => {
	it("start() requests the mic and begins recording", async () => {
		installMocks();
		const { result } = renderHook(() => useAudioTranscription());

		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledWith({
			audio: true,
		});
	});

	it("stop() uploads the recording via API.transcribeAudio (the shared, CSRF-covered client) and populates transcript exactly once, going through isTranscribing", async () => {
		installMocks();
		const transcribeAudio = vi
			.spyOn(API, "transcribeAudio")
			.mockResolvedValue({ text: "reduza a cardinalidade das métricas" });

		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		act(() => result.current.stop());

		expect(result.current.isRecording).toBe(false);
		await waitFor(() => expect(result.current.isTranscribing).toBe(true));
		await waitFor(() => expect(result.current.isTranscribing).toBe(false));

		expect(result.current.transcript).toBe(
			"reduza a cardinalidade das métricas",
		);
		// Must go through API.transcribeAudio — not a raw fetch() — so the
		// request carries X-CSRF-TOKEN via the same shared axios instance
		// every other authenticated mutation uses. A raw fetch() bypasses
		// that and gets rejected by Coder's CSRF middleware for real
		// cookie-authenticated browser sessions.
		expect(transcribeAudio).toHaveBeenCalledTimes(1);
		const uploadedBlob = transcribeAudio.mock.calls[0][0];
		expect(uploadedBlob).toBeInstanceOf(Blob);
		// Content-Type must remain the real recorded audio MIME type — never
		// converted to JSON or multipart at this layer.
		expect(uploadedBlob.type).toBe("audio/webm;codecs=opus");
		// The mic stream's tracks must be released once recording stops.
		expect(mockTracks[0].stop).toHaveBeenCalled();
	});

	it("surfaces a transcription-failed error and does NOT silently fall back when the upload fails", async () => {
		installMocks();
		vi.spyOn(API, "transcribeAudio").mockRejectedValue(new Error("500"));

		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		act(() => result.current.stop());
		await waitFor(() => expect(result.current.isTranscribing).toBe(false));

		expect(result.current.error).toBe("transcription-failed");
		expect(result.current.transcript).toBe("");
	});

	it("surfaces a not-allowed error when mic permission is denied", async () => {
		installMocks();
		denyMicAccess();

		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());

		await waitFor(() => expect(result.current.error).toBe("not-allowed"));
		expect(result.current.isRecording).toBe(false);
	});

	it("cancel() discards the recording without ever sending a transcription request", async () => {
		installMocks();
		const transcribeAudio = vi.spyOn(API, "transcribeAudio");

		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		act(() => result.current.cancel());

		expect(result.current.isRecording).toBe(false);
		expect(result.current.isTranscribing).toBe(false);
		expect(result.current.transcript).toBe("");
		expect(transcribeAudio).not.toHaveBeenCalled();
		expect(mockTracks[0].stop).toHaveBeenCalled();
	});

	it("picks the first isTypeSupported MIME type for the recorder", async () => {
		installMocks();
		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		expect(lastRecorder?.mimeType).toBe(MockMediaRecorder.supportedMimeType);
	});
});
