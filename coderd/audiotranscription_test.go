package coderd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/testutil"
)

// postAudio issues a raw POST directly (bypassing codersdk.Client.Request,
// which always forces Content-Type: application/json) so the test can send
// an arbitrary audio/* content type, exactly like a real browser upload.
func postAudio(t *testing.T, client *codersdk.Client, contentType string, body []byte) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	u, err := client.URL.Parse("/api/v2/audio-transcriptions")
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(codersdk.SessionTokenHeader, client.SessionToken())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestPostAudioTranscription_NotConfigured(t *testing.T) {
	t.Parallel()

	// Default deployment values have no transcription URL/API key set —
	// the endpoint must fail closed with 404, never silently accept audio
	// it has nowhere to send.
	client := coderdtest.New(t, nil)
	_ = coderdtest.CreateFirstUser(t, client)

	resp := postAudio(t, client, "audio/webm", []byte("fake-audio-bytes"))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPostAudioTranscription_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must never be reached for an unsupported content type")
	}))
	t.Cleanup(upstream.Close)

	client := coderdtest.New(t, &coderdtest.Options{
		DeploymentValues: coderdtest.DeploymentValues(t, func(dv *codersdk.DeploymentValues) {
			_ = dv.AI.Transcription.URL.Set(upstream.URL)
			_ = dv.AI.Transcription.APIKey.Set("test-key")
		}),
	})
	_ = coderdtest.CreateFirstUser(t, client)

	resp := postAudio(t, client, "text/plain", []byte("not audio"))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPostAudioTranscription_OversizeRejected(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must never be reached for an oversize upload")
	}))
	t.Cleanup(upstream.Close)

	client := coderdtest.New(t, &coderdtest.Options{
		DeploymentValues: coderdtest.DeploymentValues(t, func(dv *codersdk.DeploymentValues) {
			_ = dv.AI.Transcription.URL.Set(upstream.URL)
			_ = dv.AI.Transcription.APIKey.Set("test-key")
		}),
	})
	_ = coderdtest.CreateFirstUser(t, client)

	oversized := make([]byte, 26*(1<<20)) // over the 25 MiB cap
	resp := postAudio(t, client, "audio/webm", oversized)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestPostAudioTranscription_Success(t *testing.T) {
	t.Parallel()

	var gotContentType, gotModel, gotLanguage, gotAuth string
	var gotFileBytes []byte

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			switch part.FormName() {
			case "file":
				gotContentType = part.Header.Get("Content-Type")
				gotFileBytes, _ = readAllPart(part)
			case "model":
				gotModel, _ = readAllPartString(part)
			case "language":
				gotLanguage, _ = readAllPartString(part)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "reduza a cardinalidade das métricas"})
	}))
	t.Cleanup(upstream.Close)

	client := coderdtest.New(t, &coderdtest.Options{
		DeploymentValues: coderdtest.DeploymentValues(t, func(dv *codersdk.DeploymentValues) {
			_ = dv.AI.Transcription.URL.Set(upstream.URL)
			_ = dv.AI.Transcription.APIKey.Set("sk-upstream-test-key")
			_ = dv.AI.Transcription.Model.Set("whisper-1")
			_ = dv.AI.Transcription.Language.Set("pt")
		}),
	})
	_ = coderdtest.CreateFirstUser(t, client)

	audioBytes := []byte("fake-webm-audio-bytes")
	resp := postAudio(t, client, "audio/webm;codecs=opus", audioBytes)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got codersdk.AudioTranscriptionResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "reduza a cardinalidade das métricas", got.Text)

	// Prove the upstream request carried what the browser sent, plus the
	// deployment-configured hints, and the server-side-only API key — never
	// forwarded to (or visible from) the caller.
	assert.Equal(t, "Bearer sk-upstream-test-key", gotAuth)
	assert.Equal(t, audioBytes, gotFileBytes)
	assert.Equal(t, "whisper-1", gotModel)
	assert.Equal(t, "pt", gotLanguage)
	// Regression guard: multipart.Writer.CreateFormFile hardcodes
	// application/octet-stream for the file part regardless of the real
	// audio format, which every Content-Type-validating upstream (e.g.
	// Azure Speech via OmniRoute) then rejects outright. This assertion was
	// previously a no-op (`_ = gotContentType`) — gotContentType was
	// captured but never checked, which is exactly how the bug shipped.
	assert.Equal(t, "audio/webm;codecs=opus", gotContentType)
}

func readAllPart(p *multipart.Part) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(p)
	return buf.Bytes(), err
}

func readAllPartString(p *multipart.Part) (string, error) {
	b, err := readAllPart(p)
	return string(b), err
}
