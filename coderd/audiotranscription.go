package coderd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/codersdk"
)

// AudioTranscriptionMaxBytes bounds the uploaded audio blob. Generous for a
// few minutes of webm/opus voice input from a browser mic, well under any
// registered upstream STT provider's own per-request limit.
const AudioTranscriptionMaxBytes = 25 * (1 << 20) // 25 MiB

var audioContentTypeExtensions = map[string]string{
	"audio/webm": ".webm",
	"audio/ogg":  ".ogg",
	"audio/opus": ".opus",
	"audio/wav":  ".wav",
	"audio/mp4":  ".mp4",
	"audio/mpeg": ".mp3",
	"audio/flac": ".flac",
}

func audioExtensionForContentType(contentType string) string {
	// Strip any "; codecs=..." parameter — browsers commonly send
	// "audio/webm;codecs=opus" from MediaRecorder.
	base := contentType
	if idx := strings.IndexByte(base, ';'); idx != -1 {
		base = base[:idx]
	}
	base = strings.TrimSpace(strings.ToLower(base))
	if ext, ok := audioContentTypeExtensions[base]; ok {
		return ext
	}
	return ".bin"
}

// @Summary Transcribe audio
// @Description Proxies a recorded audio clip to the deployment's configured
// @Description OpenAI-Whisper-compatible transcription URL (see
// @Description --transcription-url / CODER_TRANSCRIPTION_URL). The upstream
// @Description API key is held server-side only and never reaches the
// @Description caller.
// @ID transcribe-audio
// @Security CoderSessionToken
// @Produce json
// @Tags Audio
// @Param Content-Type header string true "Audio MIME type, e.g. audio/webm"
// @Success 200 {object} codersdk.AudioTranscriptionResponse
// @Failure 400 {object} codersdk.Response
// @Failure 404 {object} codersdk.Response "Transcription is not configured on this deployment"
// @Failure 413 {object} codersdk.Response
// @Failure 502 {object} codersdk.Response
// @Router /api/v2/audio-transcriptions [post]
func (api *API) postAudioTranscription(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := api.DeploymentValues.AI.Transcription

	if cfg.URL.String() == "" || cfg.APIKey.Value() == "" {
		httpapi.Write(ctx, rw, http.StatusNotFound, codersdk.Response{
			Message: "Audio transcription is not configured on this deployment.",
		})
		return
	}

	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: fmt.Sprintf("Unsupported content type header %q — expected an audio/* MIME type.", contentType),
		})
		return
	}

	r.Body = http.MaxBytesReader(rw, r.Body, AudioTranscriptionMaxBytes)
	audioBytes, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			httpapi.Write(ctx, rw, http.StatusRequestEntityTooLarge, codersdk.Response{
				Message: "Audio file too large.",
				Detail:  fmt.Sprintf("Maximum audio size is %d bytes.", AudioTranscriptionMaxBytes),
			})
			return
		}
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "Failed to read audio from request.",
			Detail:  err.Error(),
		})
		return
	}
	if len(audioBytes) == 0 {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: "No audio data received.",
		})
		return
	}

	upstreamBody, upstreamContentType, err := buildTranscriptionUpstreamBody(audioBytes, contentType, cfg)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to build upstream transcription request.",
			Detail:  err.Error(),
		})
		return
	}

	timeout := cfg.Timeout.Value()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, cfg.URL.String(), upstreamBody)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to construct upstream transcription request.",
			Detail:  err.Error(),
		})
		return
	}
	req.Header.Set("Content-Type", upstreamContentType)
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey.Value())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "Failed to reach the transcription upstream.",
			Detail:  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	// Cap: an upstream error page or a misbehaving provider must never be
	// read unbounded into memory.
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "Failed to read the transcription upstream's response.",
		})
		return
	}

	if resp.StatusCode != http.StatusOK {
		detail := string(respBytes)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "The transcription upstream returned an error.",
			Detail:  detail,
		})
		return
	}

	var upstream struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBytes, &upstream); err != nil {
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "The transcription upstream returned an unparsable response.",
		})
		return
	}

	httpapi.Write(ctx, rw, http.StatusOK, codersdk.AudioTranscriptionResponse{
		Text: upstream.Text,
	})
}

// buildTranscriptionUpstreamBody assembles the OpenAI-Whisper-compatible
// multipart/form-data body: the audio file plus the deployment-configured
// model/language hints. Split out of postAudioTranscription for direct unit
// testing without an HTTP round trip.
func buildTranscriptionUpstreamBody(
	audioBytes []byte,
	contentType string,
	cfg codersdk.TranscriptionConfig,
) (io.Reader, string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	part, err := mw.CreateFormFile("file", "recording"+audioExtensionForContentType(contentType))
	if err != nil {
		return nil, "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		return nil, "", fmt.Errorf("write audio bytes: %w", err)
	}

	if model := cfg.Model.Value(); model != "" {
		if err := mw.WriteField("model", model); err != nil {
			return nil, "", fmt.Errorf("write model field: %w", err)
		}
	}
	if language := cfg.Language.Value(); language != "" {
		if err := mw.WriteField("language", language); err != nil {
			return nil, "", fmt.Errorf("write language field: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}

	return &body, mw.FormDataContentType(), nil
}
