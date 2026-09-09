package coderd

import (
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
)

// Regression coverage for buildTranscriptionUpstreamBody's multipart file
// part: mime/multipart.Writer.CreateFormFile hardcodes the part's
// Content-Type to application/octet-stream regardless of the actual audio
// format, which upstream providers that validate that header (e.g. Azure
// Speech, reached via OmniRoute) then reject outright. These tests exercise
// the function directly, without an HTTP round trip, per its own doc
// comment ("split out ... for direct unit testing").
func TestBuildTranscriptionUpstreamBody_FilePartContentType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		contentType string
	}{
		{name: "webm with codecs param", contentType: "audio/webm;codecs=opus"},
		{name: "mpeg", contentType: "audio/mpeg"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var cfg codersdk.TranscriptionConfig
			body, formContentType, err := buildTranscriptionUpstreamBody([]byte("fake-audio-bytes"), tc.contentType, cfg)
			require.NoError(t, err)

			_, params, err := mime.ParseMediaType(formContentType)
			require.NoError(t, err)
			mr := multipart.NewReader(body, params["boundary"])

			part, err := mr.NextPart()
			require.NoError(t, err)
			assert.Equal(t, "file", part.FormName())
			assert.Equal(t, tc.contentType, part.Header.Get("Content-Type"),
				"file part Content-Type must carry the real incoming audio format, not application/octet-stream")

			gotBytes, err := io.ReadAll(part)
			require.NoError(t, err)
			assert.Equal(t, []byte("fake-audio-bytes"), gotBytes)
		})
	}
}

// The model/language fields must round-trip unchanged by this fix — only
// the file part's header construction changed.
func TestBuildTranscriptionUpstreamBody_ModelAndLanguageFieldsUnchanged(t *testing.T) {
	t.Parallel()

	var cfg codersdk.TranscriptionConfig
	_ = cfg.Model.Set("whisper-1")
	_ = cfg.Language.Set("pt-BR")

	body, formContentType, err := buildTranscriptionUpstreamBody([]byte("fake-audio-bytes"), "audio/wav", cfg)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(formContentType)
	require.NoError(t, err)
	mr := multipart.NewReader(body, params["boundary"])

	var gotModel, gotLanguage string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		switch part.FormName() {
		case "model":
			b, _ := io.ReadAll(part)
			gotModel = string(b)
		case "language":
			b, _ := io.ReadAll(part)
			gotLanguage = string(b)
		}
	}
	assert.Equal(t, "whisper-1", gotModel)
	assert.Equal(t, "pt-BR", gotLanguage)
}

// Filenames for unrecognized content types must still fall back to a safe
// generic extension — unchanged by this fix.
func TestBuildTranscriptionUpstreamBody_UnknownContentTypeFallsBackToBinExtension(t *testing.T) {
	t.Parallel()

	var cfg codersdk.TranscriptionConfig
	body, formContentType, err := buildTranscriptionUpstreamBody([]byte("x"), "audio/x-totally-unknown", cfg)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(formContentType)
	require.NoError(t, err)
	mr := multipart.NewReader(body, params["boundary"])

	part, err := mr.NextPart()
	require.NoError(t, err)
	assert.Contains(t, part.Header.Get("Content-Disposition"), `filename="recording.bin"`)
	assert.Equal(t, "audio/x-totally-unknown", part.Header.Get("Content-Type"))
}
