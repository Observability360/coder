import { act, renderHook, waitFor } from "@testing-library/react";
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

const originalFetch = globalThis.fetch;

afterEach(() => {
	globalThis.fetch = originalFetch;
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

	it("stop() uploads the recording and populates transcript exactly once, going through isTranscribing", async () => {
		installMocks();
		globalThis.fetch = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({ text: "reduza a cardinalidade das métricas" }),
		});

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
		expect(globalThis.fetch).toHaveBeenCalledWith(
			"/api/v2/audio-transcriptions",
			expect.objectContaining({ method: "POST" }),
		);
		// The mic stream's tracks must be released once recording stops.
		expect(mockTracks[0].stop).toHaveBeenCalled();
	});

	it("surfaces a transcription-failed error and does NOT silently fall back when the upload fails", async () => {
		installMocks();
		globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500 });

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
		globalThis.fetch = vi.fn();

		const { result } = renderHook(() => useAudioTranscription());
		act(() => result.current.start());
		await waitFor(() => expect(result.current.isRecording).toBe(true));

		act(() => result.current.cancel());

		expect(result.current.isRecording).toBe(false);
		expect(result.current.isTranscribing).toBe(false);
		expect(result.current.transcript).toBe("");
		expect(globalThis.fetch).not.toHaveBeenCalled();
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
